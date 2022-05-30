package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
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

func main() {
	task := Task{
		App:     "test",
		Author:  "John",
		Project: "whoami",
		Images: []Image{
			{
				Image: "traefik/whoami",
				Tag:   "v1.8.0",
			},
		},
	}
	fmt.Println(task.send())
}
