package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
)

type Watcher struct {
	baseUrl string
	client  *http.Client
}

var (
	tag = os.Getenv("IMAGE_TAG")
)

func (watcher *Watcher) addTask(task models.Task) string {
	body, err := json.Marshal(task)
	if err != nil {
		panic(err)
	}

	url := fmt.Sprintf("%s/api/v1/tasks", watcher.baseUrl)
	request, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	response, err := watcher.client.Do(request)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	body, err = io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	if response.StatusCode != 202 {
		fmt.Printf("Something went wrong on argo-watcher side. Got the following response code %d\n", response.StatusCode)
		fmt.Printf("Body: %s\n", string(body))
		os.Exit(1)
	}

	var accepted models.TaskStatus
	err = json.Unmarshal(body, &accepted)
	if err != nil {
		panic(err)
	}

	return accepted.Id
}

func (watcher *Watcher) getTaskStatus(id string) *models.TaskStatus {
	url := fmt.Sprintf("%s/api/v1/tasks/%s", watcher.baseUrl, id)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}

	response, err := watcher.client.Do(request)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != 200 {
		fmt.Printf("Received non 200 status code (%d)\n", response.StatusCode)
		fmt.Printf("Body: %s\n", string(body))
		os.Exit(1)
	}

	var taskStatus models.TaskStatus
	err = json.Unmarshal(body, &taskStatus)
	if err != nil {
		panic(err)
	}

	return &taskStatus
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

func ClientWatcher() {
	images := getImagesList()

	watcher := Watcher{
		baseUrl: strings.TrimSuffix(os.Getenv("ARGO_WATCHER_URL"), "/"),
		client:  &http.Client{},
	}

	task := models.Task{
		App:           os.Getenv("ARGO_APP"),
		Author:        os.Getenv("COMMIT_AUTHOR"),
		Project:       os.Getenv("PROJECT_NAME"),
		ProvidedToken: os.Getenv("ARGO_WATCHER_DEPLOY_TOKEN"),
		Images:        images,
	}

	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))

	if debug {
		fmt.Printf("Got the following configuration:\n"+
			"ARGO_WATCHER_URL: %s\n"+
			"ARGO_APP: %s\n"+
			"COMMIT_AUTHOR: %s\n"+
			"PROJECT_NAME: %s\n"+
			"IMAGE_TAG: %s\n"+
			"IMAGES: %s\n\n",
			watcher.baseUrl, task.App, task.Author, task.Project, tag, task.Images)
		if task.ProvidedToken == "" {
			fmt.Println("ARGO_WATCHER_DEPLOY_TOKEN is not set, git commit will not be performed.")
		}
	}

	fmt.Printf("Waiting for %s app to be running on %s version.\n", task.App, tag)

	id := watcher.addTask(task)

	time.Sleep(5 * time.Second)

loop:
	for {
		switch taskInfo := watcher.getTaskStatus(id); taskInfo.Status {
		case models.StatusFailedMessage:
			fmt.Println("The deployment has failed, please check logs.")
			fmt.Println(taskInfo.StatusReason)
			os.Exit(1)
		case models.StatusInProgressMessage:
			fmt.Println("Application deployment is in progress...")
			time.Sleep(15 * time.Second)
		case models.StatusAppNotFoundMessage:
			fmt.Printf("Application %s does not exist.\n", task.App)
			fmt.Println(taskInfo.StatusReason)
			os.Exit(1)
		case models.StatusArgoCDUnavailableMessage:
			fmt.Println("ArgoCD is unavailable. Please investigate.")
			fmt.Println(taskInfo.StatusReason)
			os.Exit(1)
		case models.StatusDeployedMessage:
			fmt.Printf("The deployment of %s version is done.\n", tag)
			break loop
		}
	}
}
