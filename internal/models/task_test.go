package models

import (
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
