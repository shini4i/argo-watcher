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
		return fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(v)
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
