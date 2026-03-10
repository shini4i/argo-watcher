package argocd

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type ArgoApiInterface interface {
	Init(serverConfig *config.ServerConfig) error
	GetUserInfo() (*models.Userinfo, error)
	GetApplication(app string) (*models.Application, error)
}

type ArgoApi struct {
	baseUrl    url.URL
	client     *http.Client
	refreshApp bool
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
	// prepare cookie token
	cookie := &http.Cookie{
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

	// whether to refresh the app during status check
	api.refreshApp = serverConfig.ArgoRefreshApp
	log.Debug().Msgf("Refresh app set to: %t", api.refreshApp)

	return nil
}

// doGet creates a GET request for the given URL, sets the Accept header for JSON responses,
// executes it, and returns the response body bytes along with the HTTP status code.
func (api *ArgoApi) doGet(reqURL string) ([]byte, int, error) {
	req, err := api.requestFn("GET", reqURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := api.client.Do(req)
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

	body, statusCode, err := api.doGet(apiUrl)
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

func (api *ArgoApi) GetApplication(app string) (*models.Application, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/applications/%s", api.baseUrl.String(), url.PathEscape(app))

	if api.refreshApp {
		apiUrl += "?refresh=normal"
	}

	body, statusCode, err := api.doGet(apiUrl)
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
