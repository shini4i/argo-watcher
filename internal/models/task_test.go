package models

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTask_ListImages(t *testing.T) {
	task := Task{
		Images: []Image{
			{
				Image: "example",
				Tag:   "v0.2.0",
			},
			{
				Image: "example-2",
				Tag:   "v0.3.0",
			},
		},
	}

	expected := []string{"example:v0.2.0", "example-2:v0.3.0"}
	result := task.ListImages()

	assert.Equal(t, expected, result, "List of images does not match")
}

func TestTask_ListImages_Empty(t *testing.T) {
	task := Task{
		Images: []Image{},
	}

	expected := []string{}
	result := task.ListImages()

	assert.Equal(t, expected, result, "List of images does not match")
}

func TestTask_IsAppNotFoundError(t *testing.T) {
	task := Task{
		App: "test",
	}
	assert.Equal(t, true, task.IsAppNotFoundError(errors.New("applications.argoproj.io \"test\" not found")))
}

func TestTask_IsAppNotFoundError_Fail(t *testing.T) {
	task := Task{
		App: "test",
	}
	assert.Equal(t, false, task.IsAppNotFoundError(errors.New("random but very important error")))
}
