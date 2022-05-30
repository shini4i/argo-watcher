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

func (task *Task) send() string {
	body, err := json.Marshal(task)

	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", os.Getenv("ARGO_WATCHER_URL")+"/api/v1/tasks", bytes.NewBuffer(body))
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

	if response.StatusCode != 202 {
		fmt.Println("Something went wrong. Aborting...")
	}

	body, err = ioutil.ReadAll(response.Body)
	if err != nil {
		panic(err)
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
	request, err := http.NewRequest("GET", os.Getenv("ARGO_WATCHER_URL")+"/api/v1/tasks/"+id, nil)
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
	task := Task{
		App:     "whoami",
		Author:  "John",
		Project: "whoami",
		Images: []Image{
			{
				Image: "traefik/whoami",
				Tag:   "v1.8.0",
			},
		},
	}
	id := task.send()

	time.Sleep(5 * time.Second)

loop:
	for {
		switch status := task.getStatus(id); status {
		case "failed":
			fmt.Println("The deployment has failed, please check logs.")
			break loop
		case "in progress":
			fmt.Println("Application deployment is in progress..")
			time.Sleep(5 * time.Second)
		case "app not found":
			fmt.Println("Application " + task.App + " does not exist")
			break loop
		case "deployed":
			fmt.Println("done")
			break loop
		}
	}
}
