package main

import (
	"github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
	"testing"
	"time"
)

var taskId string

var (
	testArgo = Argo{
		Url:      "http://localhost:8081",
		User:     "watcher",
		Password: "test",
	}
	testClient = testArgo.Init()
	task       = m.Task{
		Created: float64(time.Now().Unix()),
		App:     "Test",
		Author:  "Test Author",
		Project: "Test Project",
		Images: []m.Image{
			{
				Image: "test",
				Tag:   "v0.0.1",
			},
		},
		Status: "in progress",
	}
)

func TestArgo_GetTaskStatus(t *testing.T) {
	taskId = testClient.AddTask(task)

	if status := testClient.GetTaskStatus(taskId); status != "in progress" {
		t.Errorf("got %s, expected %s", status, "in progress")
	}
}

func TestArgo_GetAppList(t *testing.T) {
	apps := testClient.GetAppList()

	for _, app := range apps {
		if !helpers.Contains([]string{"Test", "Test2"}, app) {
			t.Errorf("Got unexpected value %s", app)
		}
	}

	if len(apps) != 1 {
		t.Errorf("Got %d apps, but expected %d", len(apps), 2)
	}
}
