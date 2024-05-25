package client

import (
	"bytes"
	"os"

	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	clientConfig *Config
)

type Watcher struct {
	baseUrl   string
	client    *http.Client
	debugMode bool
}

// NewWatcher creates a new Watcher instance with the given base URL, timeout, and debug mode.
func NewWatcher(baseUrl string, debugMode bool, timeout time.Duration) *Watcher {
	return &Watcher{
		baseUrl:   baseUrl,
		client:    &http.Client{Timeout: timeout},
		debugMode: debugMode,
	}
}

// addTask adds a given task to the watcher, using either JWT or a DeployToken for authorization.
// It returns the task ID or an error.
func (watcher *Watcher) addTask(task models.Task, authMethod, token string) (string, error) {
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
	if authMethod != "" && token != "" {
		switch authMethod {
		case "JWT":
			request.Header.Set("Authorization", token)
		case "DeployToken":
			request.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", token)
		}
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

// getTaskStatus retrieves the status of the task identified by the given ID,
// returning a TaskStatus or an error.
func (watcher *Watcher) getTaskStatus(id string) (*models.TaskStatus, error) {
	url := fmt.Sprintf("%s/api/v1/tasks/%s", watcher.baseUrl, id)
	var taskStatus models.TaskStatus
	if err := watcher.getJSON(url, &taskStatus); err != nil {
		return nil, err
	}
	return &taskStatus, nil
}

// getWatcherConfig retrieves the watcher's server configuration,
// returning a ServerConfig or an error.
func (watcher *Watcher) getWatcherConfig() (*config.ServerConfig, error) {
	url := fmt.Sprintf("%s/api/v1/config", watcher.baseUrl)
	var serverConfig config.ServerConfig
	if err := watcher.getJSON(url, &serverConfig); err != nil {
		return nil, err
	}
	return &serverConfig, nil
}

// waitForDeployment waits for the deployment identified by the given ID,
// performing retries if necessary, and returns an error if deployment fails.
func (watcher *Watcher) waitForDeployment(id, appName, version string) error {
	retryCount := 0

	for {
		taskInfo, err := watcher.getTaskStatus(id)
		if err != nil {
			return err
		}

		switch taskInfo.Status {
		case models.StatusFailedMessage:
			return fmt.Errorf("The deployment has failed, please check logs.\n%s", taskInfo.StatusReason)
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
		case models.StatusDeployedMessage:
			log.Printf("The deployment of %s version is done.", version)
			return nil
		}
	}
}

// handleDeploymentError logs the given error,
// generates an application URL in case of deployment failure
// and exits the program with code 1.
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

// handleFatalError logs a provided error message and terminates the program with status 1.
func handleFatalError(err error, message string) {
	log.Fatalf("%s Got the following error: %s", message, err)
}

// isDeploymentOverTime checks if the deployment has exceeded the expected deployment time,
// returning a boolean value.
func isDeploymentOverTime(retryCount int, retryInterval time.Duration, expectedDeploymentTime time.Duration) bool {
	return time.Duration(retryCount)*retryInterval > expectedDeploymentTime
}

// Run initializes the client configuration, sets up the watcher,
// creates the task, adds the task to the watcher,
// waits for deployment and handles any errors in the process.
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

	var authMethod, token string
	if clientConfig.JsonWebToken != "" {
		authMethod = "JWT"
		token = clientConfig.JsonWebToken
	} else if clientConfig.Token != "" {
		authMethod = "DeployToken"
		token = clientConfig.Token
	}

	id, err := watcher.addTask(task, authMethod, token)
	if err != nil {
		handleFatalError(err, "Couldn't add task.")
	}

	// Giving Argo-Watcher some time to process the task
	time.Sleep(5 * time.Second)

	if err = watcher.waitForDeployment(id, task.App, clientConfig.Tag); err != nil {
		handleDeploymentError(watcher, task, err)
	}
}
