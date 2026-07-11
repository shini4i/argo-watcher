package argocd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	retry "github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type ArgoApiInterface interface {
	Init(serverConfig *config.ServerConfig) error
	GetUserInfo() (*models.Userinfo, error)
	GetApplication(ctx context.Context, app string, refresh bool) (*models.Application, error)
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
	log.Debug().Msg("Initializing argo-watcher client...")
	// set base url
	api.baseUrl = serverConfig.ArgoUrl

	// create cookie jar
	jar, err := api.cookieJarFn(nil)
	if err != nil {
		return err
	}
	// prepare cookie token. This is an outbound request cookie sent to the
	// ArgoCD API through the client's cookie jar, not a Set-Cookie response to a
	// browser, so G124's Secure/HttpOnly/SameSite attributes do not apply — they
	// are browser-storage directives the Go HTTP client ignores when sending.
	cookie := &http.Cookie{ // #nosec G124
		Name:  "argocd.token",
		Value: serverConfig.ArgoToken,
	}
	// set cookies
	jar.SetCookies(&api.baseUrl, []*http.Cookie{cookie})
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: serverConfig.SkipTlsVerify}, // #nosec G402
	}
	// create http client
	api.client = &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   time.Duration(serverConfig.ArgoApiTimeout) * time.Second,
	}

	log.Debug().Msgf("Timeout for ArgoCD API calls set to: %s", api.client.Timeout)

	// configure retry attempts for transient transport errors
	api.maxRetries = serverConfig.ArgoApiRetries
	log.Debug().Msgf("Max API retries set to: %d", api.maxRetries)

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
			log.Debug().
				Err(err).
				Uint("retry", n+1).
				Str("url", reqURL).
				Msg("retrying ArgoCD API request")
		}),
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close response body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	return body, resp.StatusCode, nil
}

// parseArgoErrorResponse extracts an error from a non-200 ArgoCD API response body.
// It checks the message field first, then the error field, and falls back to the raw body.
func parseArgoErrorResponse(body []byte) error {
	var argoErrorResponse models.ArgoApiErrorResponse
	if err := json.Unmarshal(body, &argoErrorResponse); err != nil {
		return fmt.Errorf("could not parse json error response: %s", body)
	}

	if argoErrorResponse.Message != "" {
		return errors.New(argoErrorResponse.Message)
	}

	if argoErrorResponse.Error != "" {
		return errors.New(argoErrorResponse.Error)
	}

	return fmt.Errorf("failed parsing argocd API response: %s", string(body))
}

func (api *ArgoApi) GetUserInfo() (*models.Userinfo, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/session/userinfo", api.baseUrl.String())

	body, statusCode, err := api.doGet(context.Background(), apiUrl)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, parseArgoErrorResponse(body)
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
		log.Error().Err(err).Msg("failed to execute request")
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, parseArgoErrorResponse(body)
	}

	var argoApp models.Application
	if err = json.Unmarshal(body, &argoApp); err != nil {
		return nil, fmt.Errorf("could not parse json response: %s", body)
	}

	return &argoApp, nil
}
