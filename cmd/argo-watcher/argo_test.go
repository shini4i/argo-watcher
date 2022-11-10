package main

import (
	"github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
	"testing"
	"time"
)

var (
	taskId  string
	task2Id string
	task3Id string
	task4Id string
)

var (
	testClient = Argo{
		Url:      "http://localhost:8081",
		User:     "watcher",
		Password: "test",
		Timeout:  "10",
	}
	task = m.Task{
		Created: float64(time.Now().Unix()),
		App:     "app",
		Author:  "Test Author",
		Project: "Test Project",
		Images: []m.Image{
			{
				Image: "app",
				Tag:   "v0.0.1",
			},
		},
		Status: "in progress",
	}
)

func TestArgo_GetTaskStatus(t *testing.T) {
	var task2 m.Task
	var task3 m.Task
	var task4 m.Task

	testClient.Init()

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

	if taskInfo, _ := testClient.state.GetTask(taskId); taskInfo.Status != "deployed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "deployed")
	}

	if taskInfo, _ := testClient.state.GetTask(task2Id); taskInfo.Status != "failed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "failed")
	}

	if taskInfo, _ := testClient.state.GetTask(task3Id); taskInfo.Status != "app not found" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "app not found")
	}

	if taskInfo, _ := testClient.state.GetTask(task4Id); taskInfo.Status != "failed" {
		t.Errorf("got %s, expected %s", taskInfo.Status, "failed")
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
