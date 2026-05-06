package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/shini4i/argo-watcher/internal/models"
)

// doRequest creates a new HTTP request and sends it using the watcher's client,
// returning the response or an error.
func (watcher *Watcher) doRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	return watcher.client.Do(req)
}

// getJSON sends a GET request to a provided URL,
// parses the JSON response and stores it in the value pointed by v.
func (watcher *Watcher) getJSON(url string, v interface{}) error {
	resp, err := watcher.doRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return serverErrorFromResponse(resp.StatusCode, body)
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

// serverErrorFromResponse builds a human-readable error from an unsuccessful
// HTTP response. It tries to decode the body as a TaskStatus to extract the
// server's `error` field; failing that, it falls back to the raw body text.
// For 401/403 it appends a hint about which env vars govern auth, since the
// most common cause is a missing or wrong token on the client side.
func serverErrorFromResponse(statusCode int, body []byte) error {
	reason := serverReason(body)
	if reason == "" {
		return fmt.Errorf("argo-watcher returned status %d", statusCode)
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return fmt.Errorf("argo-watcher returned status %d: %s "+
			"(check ARGO_WATCHER_DEPLOY_TOKEN or BEARER_TOKEN)", statusCode, reason)
	}
	return fmt.Errorf("argo-watcher returned status %d: %s", statusCode, reason)
}

// serverReason extracts a useful message from the response body. Argo-watcher
// returns errors as JSON with `error` and `status` fields (models.TaskStatus);
// if neither is present we fall back to the raw body, which still beats the
// status-code-only message we used to surface.
func serverReason(body []byte) string {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return ""
	}
	var ts models.TaskStatus
	if err := json.Unmarshal(body, &ts); err == nil {
		if ts.Error != "" {
			return ts.Error
		}
		if ts.Status != "" {
			return ts.Status
		}
	}
	return string(body)
}

// getImagesList takes a list of image names and a tag,
// then returns a list of Image structs with these properties.
func getImagesList(list []string, tag string) []models.Image {
	var images []models.Image
	for _, image := range list {
		images = append(images, models.Image{
			Image: image,
			Tag:   tag,
		})
	}
	return images
}

// createTask takes a config struct, generates the images list and returns a Task object
// filled with the respective properties from the config.
func createTask(config *Config) models.Task {
	images := getImagesList(config.Images, config.Tag)
	return models.Task{
		App:     config.App,
		Author:  config.Author,
		Project: config.Project,
		Images:  images,
		Timeout: config.TaskTimeout,
	}
}

// printClientConfiguration logs the current configuration of the client including the assigned images and tokens.
// It also warns if auth tokens are missing.
func printClientConfiguration(watcher *Watcher, task models.Task) {
	fmt.Printf("Got the following configuration:\n"+
		"ARGO_WATCHER_URL: %s\n"+
		"ARGO_APP: %s\n"+
		"COMMIT_AUTHOR: %s\n"+
		"PROJECT_NAME: %s\n"+
		"IMAGE_TAG: %s\n"+
		"IMAGES: %s\n\n",
		watcher.baseUrl, task.App, task.Author, task.Project, clientConfig.Tag, task.Images)
	if clientConfig.Token == "" && clientConfig.JsonWebToken == "" {
		fmt.Println("Neither deploy token nor JSON Web token found, git commit will not be performed")
	}
}

// generateAppUrl fetches the watcher config and uses it to construct
// and return the URL for the Argo application.
func generateAppUrl(watcher *Watcher, task models.Task) (string, error) {
	cfg, err := watcher.getWatcherConfig()
	if err != nil {
		return "", err
	}

	if cfg.ArgoUrlAlias != "" {
		return fmt.Sprintf("%s/applications/%s", cfg.ArgoUrlAlias, task.App), nil
	}
	return fmt.Sprintf("%s://%s/applications/%s", cfg.ArgoUrl.Scheme, cfg.ArgoUrl.Host, task.App), nil
}

// setupWatcher takes application configuration and initializes a new Watcher instance
// with the specified parameters.
func setupWatcher(config *Config) *Watcher {
	return NewWatcher(
		strings.TrimSuffix(config.Url, "/"),
		config.Debug,
		config.Timeout,
	)
}
