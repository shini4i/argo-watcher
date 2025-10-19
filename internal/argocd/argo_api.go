package argocd

import (
	"bytes"
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

var (
	httpNewRequest = http.NewRequest
	newCookieJar   = cookiejar.New
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
}

func (api *ArgoApi) Init(serverConfig *config.ServerConfig) error {
	log.Debug().Msg("Initializing argo-watcher client...")
	// set base url
	api.baseUrl = serverConfig.ArgoUrl

	// create cookie jar
	jar, err := newCookieJar(nil)
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

func (api *ArgoApi) GetUserInfo() (*models.Userinfo, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/session/userinfo", api.baseUrl.String())
	req, err := httpNewRequest("GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}(resp.Body)

	var userInfo models.Userinfo
	if err = json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

func (api *ArgoApi) GetApplication(app string) (*models.Application, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/applications/%s", api.baseUrl.String(), app)
	req, err := httpNewRequest("GET", apiUrl, nil)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	if api.refreshApp {
		q := req.URL.Query()
		q.Add("refresh", "normal")
		req.URL.RawQuery = q.Encode()
	}

	resp, err := api.client.Do(req)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}(resp.Body)

	if resp.StatusCode != 200 {
		var argoErrorResponse models.ArgoApiErrorResponse
		if err = json.Unmarshal(body, &argoErrorResponse); err != nil {
			return nil, fmt.Errorf("could not parse json error response: %s", body)
		}

		if argoErrorResponse.Message == "" {
			return nil, fmt.Errorf(
				"failed parsing argocd API response: %s",
				bytes.NewBuffer(body).String(),
			)
		}

		return nil, errors.New(argoErrorResponse.Message)
	}

	var argoApp models.Application
	if err = json.Unmarshal(body, &argoApp); err != nil {
		return nil, fmt.Errorf("could not parse json response: %s", body)
	}

	return &argoApp, nil
}
