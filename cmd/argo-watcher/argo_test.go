package main

import (
	"github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
	"testing"
	"time"
)

var taskId string
var task2Id string
var task3Id string
var task4Id string

var (
	testArgo = Argo{
		Url:      "http://localhost:8081",
		User:     "watcher",
		Password: "test",
	}
	testClient = testArgo.Init()
	task       = m.Task{
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

	taskId = testClient.AddTask(task)

	task2 = task
	task2.App = "app2"
	task2Id = testClient.AddTask(task2)

	task3 = task
	task3.App = "app3"
	task3Id = testClient.AddTask(task3)

	task4 = task
	task4.App = "app4"
	task4Id = testClient.AddTask(task4)

	time.Sleep(5 * time.Second)

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
