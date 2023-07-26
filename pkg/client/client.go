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

	if response.StatusCode != 202 {
		fmt.Printf("Something went wrong on argo-watcher side. Got the following response code %d\n", response.StatusCode)
		fmt.Printf("Body: %s\n", string(body))
		os.Exit(1)
	}

	var accepted models.TaskStatus
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		return "", err
	}

	return accepted.Id, nil
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
		switch taskInfo := watcher.getTaskStatus(id); taskInfo.Status {
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
