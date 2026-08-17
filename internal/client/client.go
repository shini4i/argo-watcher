package client

import (
	"bytes"
	"os"

	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shini4i/argo-watcher/internal/config"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	clientConfig *Config
)

const (
	// maxTransientRetries is how many times a GET request is retried after a
	// transient failure (network error or 5xx) before giving up. Deployments
	// can poll for many minutes, so a single blip must not abort the process.
	maxTransientRetries = 3
	defaultRetryDelay   = 2 * time.Second
)

type Watcher struct {
	baseUrl    string
	client     *http.Client
	debugMode  bool
	retryDelay time.Duration
	auth       credential
	// redirectWarning keeps the dropped-credential warning to one line per run. Every
	// status poll of a deployment takes the same redirect, so warning per request would
	// bury the rest of the CI log.
	redirectWarning sync.Once
}

const (
	// jwtHeader carries a CI JWT (BEARER_TOKEN); deployTokenHeader carries a static
	// deploy token (ARGO_WATCHER_DEPLOY_TOKEN). The server routes a request to a
	// validation strategy by which of these headers it arrives in.
	jwtHeader         = "Authorization"
	deployTokenHeader = "ARGO_WATCHER_DEPLOY_TOKEN" // #nosec G101 -- header name, not a credential
)

// credential is the header and value the client presents to argo-watcher. The zero
// value carries nothing, which is how a client with no token configured talks to a
// server that requires none.
type credential struct {
	header string
	value  string
}

// credentialFrom picks the credential to present from the client configuration,
// preferring a CI JWT over a deploy token when both are set.
//
// The JWT is sent without a "Bearer " prefix: the raw value is maskable as a GitLab
// CI variable (a prefix contains a space, which GitLab refuses to mask). A legacy
// "Bearer <jwt>" value is normalized here, so the wire format never depends on how
// BEARER_TOKEN was set. The deploy token is sent verbatim — it is an opaque secret
// that may legitimately start with anything.
func credentialFrom(config *Config) credential {
	switch {
	case config.JsonWebToken != "":
		return credential{header: jwtHeader, value: strings.TrimPrefix(config.JsonWebToken, "Bearer ")}
	case config.Token != "":
		return credential{header: deployTokenHeader, value: config.Token}
	default:
		return credential{}
	}
}

func (c credential) isSet() bool {
	return c.header != "" && c.value != ""
}

// apply sets the credential's header on request. An unset credential is a no-op, which is
// how a client with no token configured talks to a server that requires none.
func (c credential) apply(request *http.Request) {
	if !c.isSet() {
		return
	}
	request.Header.Set(c.header, c.value)
}

// NewWatcher creates a new Watcher instance with the given base URL, timeout, and debug mode.
func NewWatcher(baseUrl string, debugMode bool, timeout time.Duration) *Watcher {
	watcher := &Watcher{
		baseUrl:    baseUrl,
		debugMode:  debugMode,
		retryDelay: defaultRetryDelay,
	}
	// The redirect hook is a method so it reads the credential at request time: the
	// caller assigns it after construction (see setupWatcher).
	watcher.client = &http.Client{
		Timeout:       timeout,
		CheckRedirect: watcher.dropCredentialOnHostChange,
	}
	return watcher
}

// maxRedirects mirrors net/http's default redirect limit, which setting CheckRedirect
// replaces.
const maxRedirects = 10

// dropCredentialOnHostChange strips the deploy-token header from a redirect that
// leaves the original host. net/http does this for Authorization (so the JWT is
// already covered) but not for custom headers, and the deploy token authorizes git
// write-back for every application — it must not be handed to a host the operator did
// not configure.
//
// Dropping it turns off git write-back, which surfaces later as a rollout that fails
// blaming the image or the timeout, so the loss is reported here where the cause is still
// known. The warning covers both credentials by asking whether the outgoing request still
// carries one, rather than assuming a host change drops it: net/http compares hostnames
// with the port excluded, so it keeps Authorization across a port-only change and across
// apex-to-subdomain, both of which this function still treats as a host change.
func (watcher *Watcher) dropCredentialOnHostChange(request *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}

	if request.URL.Host == via[0].URL.Host {
		return nil
	}

	request.Header.Del(deployTokenHeader)

	// net/http has already applied its own stripping to this request, so an empty header
	// here means the credential genuinely did not survive the hop.
	if watcher.auth.isSet() && request.Header.Get(watcher.auth.header) == "" {
		watcher.redirectWarning.Do(func() {
			// %q, not %s: the redirect target comes from the server's Location header,
			// which is exactly the untrusted input this function guards against. Quoting
			// escapes any control character rather than letting it forge a log line.
			// #nosec G706 -- a host cannot carry CR/LF to forge a log line: a raw one
			// cannot survive an HTTP header value, and url.Parse rejects the escaped form
			// ("invalid URL escape %0a"), so Location never parses and this hook is never
			// reached. The %q above covers it regardless.
			log.Printf("warning: %q redirected to %q. A credential is not carried across a host change, "+
				"so this deployment will proceed without git write-back. "+
				"Point ARGO_WATCHER_URL at the host that serves the API.",
				via[0].URL.Host, request.URL.Host)
		})
	}

	return nil
}

// addTask presents the watcher's credential and returns the new task ID.
func (watcher *Watcher) addTask(task models.Task) (string, error) {
	requestBody, err := json.Marshal(task)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/v1/tasks", watcher.baseUrl)

	request, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	watcher.auth.apply(request)

	// Print the equivalent cURL command for troubleshooting. Redact the auth
	// headers so the JWT / deploy token is never written to logs (e.g. CI job
	// output), which are often persisted and widely readable.
	if curlCommand, err := helpers.CurlCommandFromRequest(request, jwtHeader, deployTokenHeader); err != nil {
		log.Printf("Couldn't get cURL command. Got the following error: %s", err)
	} else if watcher.debugMode {
		log.Printf("Adding task to argo-watcher. Equivalent cURL command: %s\n", curlCommand)
	}

	response, err := watcher.client.Do(request)
	if err != nil {
		return "", err
	}

	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("warning: failed to close response body: %v", err)
		}
	}()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	if response.StatusCode != http.StatusAccepted {
		return "", serverErrorFromResponse(response.StatusCode, responseBody)
	}

	var accepted models.TaskStatus
	err = json.Unmarshal(responseBody, &accepted)
	if err != nil {
		return "", err
	}

	return accepted.Id, nil
}

func (watcher *Watcher) getTaskStatus(id string) (*models.TaskStatus, error) {
	url := fmt.Sprintf("%s/api/v1/tasks/%s", watcher.baseUrl, id)
	var taskStatus models.TaskStatus
	if err := watcher.getJSON(url, &taskStatus); err != nil {
		return nil, err
	}
	return &taskStatus, nil
}

func (watcher *Watcher) getWatcherConfig() (*config.ServerConfig, error) {
	url := fmt.Sprintf("%s/api/v1/config", watcher.baseUrl)
	var serverConfig config.ServerConfig
	if err := watcher.getJSON(url, &serverConfig); err != nil {
		return nil, err
	}
	return &serverConfig, nil
}

func (watcher *Watcher) waitForDeployment(id, appName, version string) error {
	retryCount := 0

	for {
		taskInfo, err := watcher.getTaskStatus(id)
		if err != nil {
			return err
		}

		switch taskInfo.Status {
		case models.StatusFailedMessage:
			return fmt.Errorf("The deployment has failed. See the reason below.\n%s", taskInfo.StatusReason)
		case models.StatusCancelledMessage:
			return fmt.Errorf("The deployment was cancelled because a newer deployment superseded it.\n%s", taskInfo.StatusReason)
		case models.StatusInProgressMessage:
			if !isDeploymentOverTime(retryCount, clientConfig.RetryInterval, clientConfig.ExpectedDeploymentTime) {
				log.Println("Application deployment is in progress...")
			} else {
				log.Println("Application deployment is taking longer than expected, it might be worth checking ArgoCD UI...")
			}
			retryCount++
			time.Sleep(clientConfig.RetryInterval)
		case models.StatusAppNotFoundMessage:
			return fmt.Errorf("Application %s does not exist.\n%s", appName, taskInfo.StatusReason)
		case models.StatusArgoCDUnavailableMessage:
			return fmt.Errorf("ArgoCD is unavailable. Please investigate.\n%s", taskInfo.StatusReason)
		case models.StatusAborted:
			return fmt.Errorf("The deployment was aborted before its outcome could be confirmed. See the reason below.\n%s", taskInfo.StatusReason)
		case models.StatusDeployedMessage:
			log.Printf("The deployment of %s version is done.", version)
			return nil
		default:
			// Treat any status this client does not recognize (e.g. one added by a
			// newer server) as terminal. Without this the loop would re-poll with no
			// delay on an unknown status, hammering the server and never returning.
			return fmt.Errorf("Received unexpected deployment status %q from the server; the client may be out of date.\n%s", taskInfo.Status, taskInfo.StatusReason)
		}
	}
}

func handleDeploymentError(watcher *Watcher, task models.Task, err error) {
	log.Println(err)
	if strings.Contains(err.Error(), "The deployment has failed") {
		appUrl, err := generateAppUrl(watcher, task)
		if err != nil {
			handleFatalError(err, "Couldn't generate app URL.")
		}
		log.Fatalf("To get more information about the problem, please check ArgoCD UI: %s\n", appUrl)
	}
	os.Exit(1)
}

func handleFatalError(err error, message string) {
	log.Fatalf("%s Got the following error: %s", message, err)
}

func isDeploymentOverTime(retryCount int, retryInterval time.Duration, expectedDeploymentTime time.Duration) bool {
	return time.Duration(retryCount)*retryInterval > expectedDeploymentTime
}

// Run is the client entrypoint: it builds the task, submits it, and waits for the deployment.
func Run() {
	var err error

	if clientConfig, err = NewClientConfig(); err != nil {
		log.Fatalf("Couldn't get client configuration. Got the following error: %s", err)
	}

	watcher := setupWatcher(clientConfig)
	task := createTask(clientConfig)

	if watcher.debugMode {
		printClientConfiguration(watcher, task)
	}

	log.Printf("Waiting for %s app to be running on %s version.\n", task.App, clientConfig.Tag)

	id, err := watcher.addTask(task)
	if err != nil {
		handleFatalError(err, "Couldn't add task.")
	}

	time.Sleep(5 * time.Second)

	if err = watcher.waitForDeployment(id, task.App, clientConfig.Tag); err != nil {
		handleDeploymentError(watcher, task, err)
	}
}
