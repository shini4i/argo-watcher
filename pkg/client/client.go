package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	envConfig "github.com/kelseyhightower/envconfig"
	"github.com/shini4i/argo-watcher/internal/models"
)

type Config struct {
	ArgoWatcherUrl string `required:"true" envconfig:"ARGO_WATCHER_URL"`
	Images         string `required:"true" envconfig:"IMAGES"`
	Tag            string `required:"true" envconfig:"IMAGE_TAG"`
	App            string `required:"true" envconfig:"ARGO_APP"`
	Author         string `required:"true" envconfig:"COMMIT_AUTHOR"`
	Project        string `required:"true" envconfig:"PROJECT_NAME"`
	Debug          bool   `default:"false" envconfig:"DEBUG"`
}

func NewClientConfig() (*Config, error) {
	var config Config
	err := envConfig.Process("", &config)
	return &config, err
}

type Watcher struct {
	baseUrl string
	client  *http.Client
}

func (watcher *Watcher) addTask(task models.Task) (string, error) {
	body, err := json.Marshal(task)
	if err != nil {
		panic(err)
	}

	url := fmt.Sprintf("%s/api/v1/tasks", watcher.baseUrl)
	request, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

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

	var accepted models.TaskStatus
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
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

	var taskStatus models.TaskStatus
	if err := json.NewDecoder(response.Body).Decode(&taskStatus); err != nil {
		return nil, err
	}

	return &taskStatus, nil
}

func getImagesList(config *Config) []models.Image {
	var images []models.Image
	for _, image := range strings.Split(config.Images, ",") {
		images = append(images, models.Image{
			Image: image,
			Tag:   config.Tag,
		})
	}
	return images
}

func printDebugInformation(config *Config, task *models.Task) {
	fmt.Printf("Got the following configuration:\n"+
		"ARGO_WATCHER_URL: %s\n"+
		"ARGO_APP: %s\n"+
		"COMMIT_AUTHOR: %s\n"+
		"PROJECT_NAME: %s\n"+
		"IMAGE_TAG: %s\n"+
		"IMAGES: %s\n\n",
		config.ArgoWatcherUrl, task.App, task.Author, task.Project, config.Tag, task.Images)
}

func WatcherClient() {
	config, err := NewClientConfig()
	if err != nil {
		panic(err)
	}

	images := getImagesList(config)

	watcher := Watcher{
		baseUrl: strings.TrimSuffix(config.ArgoWatcherUrl, "/"),
		client:  &http.Client{},
	}

	task := models.Task{
		App:     config.App,
		Author:  config.Author,
		Project: config.Project,
		Images:  images,
	}

	if config.Debug {
		printDebugInformation(config, &task)
	}

	id, err := watcher.addTask(task)
	if err != nil {
		fmt.Printf("Something went wrong on argo-watcher side. Got the following error %s\n", err.Error())
		os.Exit(1)
	}

	fmt.Printf("Waiting for %s app to be running on %s version.\n", task.App, config.Tag)

	time.Sleep(5 * time.Second)

loop:
	for {
		taskInfo, err := watcher.getTaskStatus(id)
		if err != nil {
			log.Fatal(err)
		}

		switch taskInfo.Status {
		case models.StatusFailedMessage:
			fmt.Println("The deployment has failed, please check logs.")
			fmt.Println(taskInfo.StatusReason)
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
			fmt.Printf("The deployment of %s version is done.\n", config.Tag)
			break loop
		}
	}
}
