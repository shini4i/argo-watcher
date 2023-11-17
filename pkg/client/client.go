package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	tag      = os.Getenv("IMAGE_TAG")
	debug, _ = strconv.ParseBool(os.Getenv("DEBUG"))
)

type Watcher struct {
	baseUrl   string
	client    *http.Client
	debugMode bool
	timeout   time.Duration
}

func NewWatcher(baseUrl string, debugMode bool, timeout time.Duration) *Watcher {
	return &Watcher{
		baseUrl:   baseUrl,
		client:    &http.Client{Timeout: timeout},
		debugMode: debugMode,
		timeout:   timeout,
	}
}

func (watcher *Watcher) doRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), watcher.timeout)
	defer cancel()

	req = req.WithContext(ctx)

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

func (watcher *Watcher) addTask(task models.Task, token string) (string, error) {
	// Marshal the task into JSON
	requestBody, err := json.Marshal(task)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/v1/tasks", watcher.baseUrl)

	// Create a new HTTP request with the JSON responseBody
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	// Set the deploy token header if provided
	if token != "" {
		request.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", token)
	}

	// Print the equivalent cURL command for troubleshooting
	if curlCommand, err := helpers.CurlCommandFromRequest(request); err != nil {
		log.Printf("Couldn't get cURL command. Got the following error: %s", err)
	} else if watcher.debugMode {
		log.Printf("Adding task to argo-watcher. Equivalent cURL command: %s\n", curlCommand)
	}

	// Send the HTTP request
	response, err := watcher.client.Do(request)
	if err != nil {
		return "", err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	// Check the HTTP status code for success
	if response.StatusCode != http.StatusAccepted {
		errMsg := fmt.Sprintf("Something went wrong on argo-watcher side. Got the following response code %d", response.StatusCode)
		return "", errors.New(errMsg)
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

func (watcher *Watcher) waitForDeployment(id, appName string) error {
	for {
		taskInfo, err := watcher.getTaskStatus(id)
		if err != nil {
			return err
		}

		switch taskInfo.Status {
		case models.StatusFailedMessage:
			return fmt.Errorf("The deployment has failed, please check logs.\n%s", taskInfo.StatusReason)
		case models.StatusInProgressMessage:
			log.Println("Application deployment is in progress...")
			time.Sleep(15 * time.Second)
		case models.StatusAppNotFoundMessage:
			return fmt.Errorf("Application %s does not exist.\n%s", appName, taskInfo.StatusReason)
		case models.StatusArgoCDUnavailableMessage:
			return fmt.Errorf("ArgoCD is unavailable. Please investigate.\n%s", taskInfo.StatusReason)
		case models.StatusDeployedMessage:
			log.Printf("The deployment of %s version is done.\n", tag)
			return nil
		}
	}
}

func getImagesList() []models.Image {
	var images []models.Image
	for _, image := range strings.Split(os.Getenv("IMAGES"), ",") {
		images = append(images, models.Image{
			Image: image,
			Tag:   tag,
		})
	}
	return images
}

func Run() {
	images := getImagesList()

	watcher := NewWatcher(
		strings.TrimSuffix(os.Getenv("ARGO_WATCHER_URL"), "/"),
		debug,
		30*time.Second,
	)

	task := models.Task{
		App:     os.Getenv("ARGO_APP"),
		Author:  os.Getenv("COMMIT_AUTHOR"),
		Project: os.Getenv("PROJECT_NAME"),
		Images:  images,
	}

	deployToken := os.Getenv("ARGO_WATCHER_DEPLOY_TOKEN")

	if watcher.debugMode {
		fmt.Printf("Got the following configuration:\n"+
			"ARGO_WATCHER_URL: %s\n"+
			"ARGO_APP: %s\n"+
			"COMMIT_AUTHOR: %s\n"+
			"PROJECT_NAME: %s\n"+
			"IMAGE_TAG: %s\n"+
			"IMAGES: %s\n\n",
			watcher.baseUrl, task.App, task.Author, task.Project, tag, task.Images)
		if deployToken == "" {
			fmt.Println("ARGO_WATCHER_DEPLOY_TOKEN is not set, git commit will not be performed.")
		}
	}

	log.Printf("Waiting for %s app to be running on %s version.\n", task.App, tag)

	id, err := watcher.addTask(task, deployToken)
	if err != nil {
		log.Printf("Couldn't add task. Got the following error: %s", err)
		os.Exit(1)
	}

	// Giving Argo-Watcher some time to process the task
	time.Sleep(5 * time.Second)

	if err := watcher.waitForDeployment(id, task.App); err != nil {
		cfg, err := watcher.getWatcherConfig()
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}

		var appUrl string

		if cfg.ArgoUrlAlias != "" {
			appUrl = fmt.Sprintf("%s/applications/%s", cfg.ArgoUrlAlias, task.App)
		} else {
			appUrl = fmt.Sprintf("%s://%s/applications/%s", cfg.ArgoUrl.Scheme, cfg.ArgoUrl.Host, task.App)
		}

		log.Println(err)
		log.Printf("To get more information about the problem, please check ArgoCD UI: %s\n", appUrl)
		os.Exit(1)
	}
}
