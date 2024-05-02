package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/shini4i/argo-watcher/internal/models"
)

func (watcher *Watcher) doRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	return watcher.client.Do(req)
}

func (watcher *Watcher) getJSON(url string, v interface{}) error {
	resp, err := watcher.doRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

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

func createTask(config *ClientConfig) models.Task {
	images := getImagesList(config.Images, config.Tag)
	return models.Task{
		App:     config.App,
		Author:  config.Author,
		Project: config.Project,
		Images:  images,
		Timeout: config.TaskTimeout,
	}
}

func printClientConfiguration(watcher *Watcher, task models.Task) {
	fmt.Printf("Got the following configuration:\n"+
		"ARGO_WATCHER_URL: %s\n"+
		"ARGO_APP: %s\n"+
		"COMMIT_AUTHOR: %s\n"+
		"PROJECT_NAME: %s\n"+
		"IMAGE_TAG: %s\n"+
		"IMAGES: %s\n\n",
		watcher.baseUrl, task.App, task.Author, task.Project, clientConfig.Tag, task.Images)
	if clientConfig.Token == "" {
		fmt.Println("ARGO_WATCHER_DEPLOY_TOKEN is not set, git commit will not be performed.")
	}
}

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

func setupWatcher(config *ClientConfig) *Watcher {
	return NewWatcher(
		strings.TrimSuffix(config.Url, "/"),
		config.Debug,
		config.Timeout,
	)
}
