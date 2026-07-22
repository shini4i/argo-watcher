package argocd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	retry "github.com/avast/retry-go/v4"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type ArgoApiInterface interface {
	Init(serverConfig *config.ServerConfig) error
	GetUserInfo() (*models.Userinfo, error)
	GetApplication(ctx context.Context, app string, refresh bool) (*models.Application, error)
	GetResourceTree(ctx context.Context, app string) (*models.ApplicationTree, error)
}

type ArgoApi struct {
	baseUrl    url.URL
	client     *http.Client
	maxRetries uint
	// requestFn allows injecting a custom HTTP request constructor for testing.
	requestFn func(method, url string, body io.Reader) (*http.Request, error)
	// cookieJarFn allows injecting a custom cookie jar factory for testing.
	cookieJarFn func(o *cookiejar.Options) (*cookiejar.Jar, error)
}

// NewArgoApi constructs an ArgoApi with default HTTP helpers.
func NewArgoApi() *ArgoApi {
	return &ArgoApi{
		requestFn:   http.NewRequest,
		cookieJarFn: cookiejar.New,
	}
}

func (api *ArgoApi) Init(serverConfig *config.ServerConfig) error {
	slog.Debug("Initializing argo-watcher client...")
	api.baseUrl = serverConfig.ArgoUrl

	jar, err := api.cookieJarFn(nil)
	if err != nil {
		return err
	}
	// This is an outbound request cookie sent to the ArgoCD API through the
	// client's cookie jar, not a Set-Cookie response to a browser, so G124's
	// Secure/HttpOnly/SameSite attributes do not apply — the Go HTTP client
	// ignores those browser-storage directives when sending.
	cookie := &http.Cookie{ // #nosec G124
		Name:  "argocd.token",
		Value: serverConfig.ArgoToken,
	}
	jar.SetCookies(&api.baseUrl, []*http.Cookie{cookie})
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: serverConfig.SkipTlsVerify}, // #nosec G402
	}
	api.client = &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   time.Duration(serverConfig.ArgoApiTimeout) * time.Second,
	}

	slog.Debug("Timeout for ArgoCD API calls set", "timeout", api.client.Timeout)

	api.maxRetries = serverConfig.ArgoApiRetries
	slog.Debug("Max API retries set", "max_retries", api.maxRetries)

	return nil
}

// doGet creates a GET request for the given URL, sets the Accept header for JSON responses,
// executes it with retry logic for transient transport errors, and returns the response body
// bytes along with the HTTP status code. Only the HTTP round-trip is retried; request creation
// and body reading errors are not retried. HTTP error responses (4xx, 5xx) are valid API
// responses and are returned as-is without retry.
//
// The supplied context bounds both the in-flight HTTP round-trip and the retry/backoff loop:
// once it is cancelled or its deadline is exceeded, any pending request is aborted and no
// further attempts are made, so a slow ArgoCD cannot stretch a single call past the caller's
// deadline.
func (api *ArgoApi) doGet(ctx context.Context, reqURL string) ([]byte, int, error) {
	req, err := api.requestFn("GET", reqURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req = req.WithContext(ctx)
	req.Header.Set("Accept", "application/json")

	// Safe to reuse across retries: GET request has no body that would be consumed.
	var resp *http.Response
	err = retry.Do(
		func() error {
			var doErr error
			resp, doErr = api.client.Do(req)
			return doErr
		},
		retry.Context(ctx),
		retry.Attempts(api.maxRetries),
		retry.Delay(time.Second),
		retry.MaxDelay(30*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			slog.Debug("retrying ArgoCD API request", "error", err, "retry", n+1, "url", reqURL)
		}),
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Error("failed to close response body", "error", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	return body, resp.StatusCode, nil
}

// ArgoAPIError is a non-2xx HTTP response from the ArgoCD API. It carries the
// status code so callers can tell a 5xx outage from a 4xx application error.
// Error returns ArgoCD's message so IsAppNotFoundError keeps working.
type ArgoAPIError struct {
	StatusCode int
	Message    string
}

func (e *ArgoAPIError) Error() string {
	return e.Message
}

// parseArgoErrorResponse builds an *ArgoAPIError from a non-200 ArgoCD API response.
// It checks the message field first, then the error field, and falls back to the raw body.
func parseArgoErrorResponse(statusCode int, body []byte) error {
	var argoErrorResponse models.ArgoApiErrorResponse
	if err := json.Unmarshal(body, &argoErrorResponse); err != nil {
		return &ArgoAPIError{StatusCode: statusCode, Message: fmt.Sprintf("could not parse json error response: %s", body)}
	}

	if argoErrorResponse.Message != "" {
		return &ArgoAPIError{StatusCode: statusCode, Message: argoErrorResponse.Message}
	}

	if argoErrorResponse.Error != "" {
		return &ArgoAPIError{StatusCode: statusCode, Message: argoErrorResponse.Error}
	}

	return &ArgoAPIError{StatusCode: statusCode, Message: fmt.Sprintf("failed parsing argocd API response: %s", string(body))}
}

func (api *ArgoApi) GetUserInfo() (*models.Userinfo, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/session/userinfo", api.baseUrl.String())

	body, statusCode, err := api.doGet(context.Background(), apiUrl)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, parseArgoErrorResponse(statusCode, body)
	}

	var userInfo models.Userinfo
	if err = json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// GetApplication fetches the named ArgoCD application. When refresh is true the request asks
// ArgoCD to reconcile the app first (?refresh=normal). The context bounds the request and its
// retry loop so a caller polling under a deadline is never blocked past that deadline.
func (api *ArgoApi) GetApplication(ctx context.Context, app string, refresh bool) (*models.Application, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/applications/%s", api.baseUrl.String(), url.PathEscape(app))

	if refresh {
		apiUrl += "?refresh=normal"
	}

	body, statusCode, err := api.doGet(ctx, apiUrl)
	if err != nil {
		slog.Error("failed to execute request", "error", err)
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, parseArgoErrorResponse(statusCode, body)
	}

	var argoApp models.Application
	if err = json.Unmarshal(body, &argoApp); err != nil {
		return nil, fmt.Errorf("could not parse json response: %s", body)
	}

	return &argoApp, nil
}

// GetResourceTree fetches the live resource tree of the named ArgoCD application. It is the only
// source that exposes descendant resources — notably the Pods, whose health carries the actual
// failure cause (ImagePullBackOff, CrashLoopBackOff) absent from the application's top-level
// Status.Resources. The context bounds the request so a caller under a deadline is never blocked
// past it. It is called only on the failure path to enrich the reported reason.
func (api *ArgoApi) GetResourceTree(ctx context.Context, app string) (*models.ApplicationTree, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/applications/%s/resource-tree", api.baseUrl.String(), url.PathEscape(app))

	// No logging here: this call is best-effort (the caller swallows the error to enrich a
	// failure reason), so a transport hiccup must not surface as an ERROR-level line. The caller
	// logs at Debug. The unmarshal error is wrapped with %w so the underlying cause is preserved.
	body, statusCode, err := api.doGet(ctx, apiUrl)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, parseArgoErrorResponse(statusCode, body)
	}

	var tree models.ApplicationTree
	if err = json.Unmarshal(body, &tree); err != nil {
		return nil, fmt.Errorf("could not parse resource-tree response: %w", err)
	}

	return &tree, nil
}
