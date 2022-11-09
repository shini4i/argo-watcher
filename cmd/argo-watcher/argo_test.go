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

	time.Sleep(1 * time.Second)

	if status := testClient.GetTaskStatus(taskId); status != "deployed" {
		t.Errorf("got %s, expected %s", status, "deployed")
	}

	if status := testClient.GetTaskStatus(task2Id); status != "failed" {
		t.Errorf("got %s, expected %s", status, "failed")
	}

	if status := testClient.GetTaskStatus(task3Id); status != "app not found" {
		t.Errorf("got %s, expected %s", status, "app not found")
	}

	if status := testClient.GetTaskStatus(task4Id); status != "failed" {
		t.Errorf("got %s, expected %s", status, "failed")
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
