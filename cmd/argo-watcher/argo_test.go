package main

import (
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/conf"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	taskId  string
	task2Id string
	task3Id string
	task4Id string
)

var (
	testClient = Argo{}
	task = models.Task{
		Created: float64(time.Now().Unix()),
		App:     "app",
		Author:  "Test Author",
		Project: "Test Project",
		Images: []models.Image{
			{
				Image: "app",
				Tag:   "v0.0.1",
			},
		},
		Status: "in progress",
	}
)

func TestArgo_GetTask(t *testing.T) {
	var task2 models.Task
	var task3 models.Task
	var task4 models.Task

	
	config := &conf.ServerConfig{StateType: "in-memory", ArgoUrl: "http://localhost:8081", ArgoToken: "dummy", ArgoTimeout: "10"}
	state, err := state.NewState(config)
	if err != nil {
		t.Error(err)
	}
	
	api := ArgoApi{}
	if err := api.Init(config); err != nil {
		t.Error(err)
	}

	metrics := Metrics{}
	metrics.Init()

	testClient.Init(&state, &api, &metrics, 0)

	taskId, _ = testClient.AddTask(task)

	task2 = task
	task2.App = "app2"
	task2Id, _ = testClient.AddTask(task2)

	task3 = task
	task3.App = "app3"
	task3Id, _ = testClient.AddTask(task3)

	task4 = task
	task4.App = "app4"
	task4Id, _ = testClient.AddTask(task4)

	time.Sleep(2 * time.Second)

	const errorMessageTemplate = "got %s, expected %s"

	if taskInfo, _ := testClient.state.GetTask(taskId); taskInfo.Status != "deployed" {
		t.Errorf(errorMessageTemplate, taskInfo.Status, "deployed")
	}

	if taskInfo, _ := testClient.state.GetTask(task2Id); taskInfo.Status != "failed" {
		t.Errorf(errorMessageTemplate, taskInfo.Status, "failed")
	}

	if taskInfo, _ := testClient.state.GetTask(task3Id); taskInfo.Status != "app not found" {
		t.Errorf(errorMessageTemplate, taskInfo.Status, "app not found")
	}

	if taskInfo, _ := testClient.state.GetTask(task4Id); taskInfo.Status != "failed" {
		t.Errorf(errorMessageTemplate, taskInfo.Status, "failed")
	}
}

func TestArgo_GetAppList(t *testing.T) {
	apps := testClient.GetAppList()

	for _, app := range apps {
		if !helpers.Contains([]string{"app", "app2", "app3", "app4"}, app) {
			t.Errorf("Got unexpected value %s", app)
		}
	}

	if len(apps) != 4 {
		t.Errorf("Got %d apps, but expected %d", len(apps), 4)
	}
}
