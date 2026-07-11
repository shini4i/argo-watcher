package models

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTask_RefreshUnmarshal verifies the per-task refresh override (issue #334) is optional and
// backward compatible: a payload from an old client (no "refresh" field) yields a nil pointer, while
// explicit true/false round-trip as set. A nil pointer lets the server fall back to its instance default.
func TestTask_RefreshUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		assert  func(t *testing.T, refresh *bool)
	}{
		{
			name:    "field omitted (old client)",
			payload: `{"app":"demo","author":"ci","project":"Demo","images":[]}`,
			assert:  func(t *testing.T, refresh *bool) { assert.Nil(t, refresh) },
		},
		{
			name:    "explicit false",
			payload: `{"app":"demo","author":"ci","project":"Demo","images":[],"refresh":false}`,
			assert: func(t *testing.T, refresh *bool) {
				require.NotNil(t, refresh)
				assert.False(t, *refresh)
			},
		},
		{
			name:    "explicit true",
			payload: `{"app":"demo","author":"ci","project":"Demo","images":[],"refresh":true}`,
			assert: func(t *testing.T, refresh *bool) {
				require.NotNil(t, refresh)
				assert.True(t, *refresh)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var task Task
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &task))
			tc.assert(t, task.Refresh)
		})
	}
}

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
