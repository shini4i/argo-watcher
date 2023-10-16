package client

import (
	"bytes"
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

	"github.com/shini4i/argo-watcher/internal/models"
)

type Watcher struct {
	baseUrl string
	client  *http.Client
}

var (
	tag = os.Getenv("IMAGE_TAG")
)

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
	curlCommand := helpers.CurlCommandFromRequest(request)
	log.Printf("Equivalent cURL command:\n%s\n", curlCommand)

	log.Printf("Request Headers: %+v", request.Header)

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
		errMsg := fmt.Sprintf("Something went wrong on argo-watcher side. Got the following response code %d\n", response.StatusCode)
		errMsg += fmt.Sprintf("Body: %s\n", string(responseBody))
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
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	response, err := watcher.client.Do(request)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	if response.StatusCode != http.StatusOK {
		log.Printf("Received non-200 status code (%d)\n", response.StatusCode)
		body, _ := io.ReadAll(response.Body)
		log.Printf("Body: %s\n", string(body))
		return nil, fmt.Errorf("received non-200 status code: %d", response.StatusCode)
	}

	var taskStatus models.TaskStatus
	if err := json.NewDecoder(response.Body).Decode(&taskStatus); err != nil {
		return nil, err
	}

	return &taskStatus, nil
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

	watcher := Watcher{
		baseUrl: strings.TrimSuffix(os.Getenv("ARGO_WATCHER_URL"), "/"),
		client:  &http.Client{},
	}

	task := models.Task{
		App:     os.Getenv("ARGO_APP"),
		Author:  os.Getenv("COMMIT_AUTHOR"),
		Project: os.Getenv("PROJECT_NAME"),
		Images:  images,
	}

	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))

	deployToken := os.Getenv("ARGO_WATCHER_DEPLOY_TOKEN")

	if debug {
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

	fmt.Printf("Waiting for %s app to be running on %s version.\n", task.App, tag)

	id, err := watcher.addTask(task, deployToken)
	if err != nil {
		log.Panicf("Couldn't add task. Got the following error: %s", err)
	}

	// Giving Argo-Watcher some time to process the task
	time.Sleep(5 * time.Second)

loop:
	for {
		taskInfo, err := watcher.getTaskStatus(id)
		if err != nil {
			log.Panicf("Couldn't get task status. Got the following error: %s", err)
		}

		switch taskInfo.Status {
		case models.StatusFailedMessage:
			log.Panicf("The deployment has failed, please check logs.\n%s", taskInfo.StatusReason)
		case models.StatusInProgressMessage:
			log.Println("Application deployment is in progress...")
			time.Sleep(15 * time.Second)
		case models.StatusAppNotFoundMessage:
			log.Panicf("Application %s does not exist.\n%s", task.App, taskInfo.StatusReason)
		case models.StatusArgoCDUnavailableMessage:
			log.Panicf("ArgoCD is unavailable. Please investigate.\n%s", taskInfo.StatusReason)
		case models.StatusDeployedMessage:
			log.Printf("The deployment of %s version is done.\n", tag)
			break loop
		}
	}
}
