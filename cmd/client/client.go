package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Image struct {
	Image string `json:"image"`
	Tag   string `json:"tag"`
}

type Task struct {
	App     string  `json:"app"`
	Author  string  `json:"author"`
	Project string  `json:"project"`
	Images  []Image `json:"images"`
}

var (
	url = os.Getenv("ARGO_WATCHER_URL")
)

func (task *Task) send() string {
	body, err := json.Marshal(task)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", url+"/api/v1/tasks", bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	body, err = ioutil.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	if response.StatusCode != 202 {
		fmt.Printf("Something went wrong on argo-watcher side. Got the following response code %d\n", response.StatusCode)
		fmt.Printf("Body: %s\n", string(body))
		os.Exit(1)
	}

	type AcceptedTask struct {
		Status string `json:"status"`
		Id     string `json:"id"`
	}

	var accepted AcceptedTask
	err = json.Unmarshal(body, &accepted)
	if err != nil {
		panic(err)
	}

	return accepted.Id
}

func (task *Task) getStatus(id string) string {
	request, err := http.NewRequest("GET", url+"/api/v1/tasks/"+id, nil)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(response.Body)

	body, err := ioutil.ReadAll(response.Body)

	if response.StatusCode != 200 {
		fmt.Printf("Received non 200 status code (%d)\n", response.StatusCode)
		fmt.Printf("Body: %s\n", string(body))
		os.Exit(1)
	}

	type TaskStatus struct {
		Status string `json:"status"`
	}

	var accepted TaskStatus
	err = json.Unmarshal(body, &accepted)
	if err != nil {
		panic(err)
	}

	return accepted.Status
}

func main() {
	tag := os.Getenv("IMAGE_TAG")
	var images []Image

	for _, image := range strings.Split(os.Getenv("IMAGES"), ",") {
		images = append(images, Image{
			image,
			tag,
		})
	}

	task := Task{
		App:     os.Getenv("ARGO_APP"),
		Author:  os.Getenv("COMMIT_AUTHOR"),
		Project: os.Getenv("PROJECT_NAME"),
		Images:  images,
	}

	fmt.Printf("Waiting for %s app to be running on %s version.\n", task.App, tag)

	url = strings.TrimSuffix(url, "/")

	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))

	if debug {
		fmt.Printf("Got the following configuration:\n"+
			"ARGO_WATCHER_URL: %s\n"+
			"ARGO_APP: %s\n"+
			"COMMIT_AUTHOR: %s\n"+
			"PROJECT_NAME: %s\n"+
			"IMAGE_TAG: %s\n"+
			"IMAGES: %s\n",
			os.Getenv("ARGO_WATCHER_URL"), task.App, task.Author, task.Project, tag, os.Getenv("IMAGES"))
	}

	id := task.send()

	time.Sleep(5 * time.Second)

loop:
	for {
		switch status := task.getStatus(id); status {
		case "failed":
			fmt.Println("The deployment has failed, please check logs.")
			os.Exit(1)
		case "in progress":
			fmt.Println("Application deployment is in progress...")
			time.Sleep(15 * time.Second)
		case "app not found":
			fmt.Printf("Application %s does not exist.\n", task.App)
			os.Exit(1)
		case "ArgoCD is unavailable":
			fmt.Println("ArgoCD is unavailable. Please investigate.")
			os.Exit(1)
		case "deployed":
			fmt.Printf("The deployment of %s version is done.\n", tag)
			break loop
		}
	}
}
