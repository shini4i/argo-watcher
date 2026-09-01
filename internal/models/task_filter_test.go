package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTaskMatchesSearch(t *testing.T) {
	task := Task{
		App:     "Checkout-API",
		Author:  "Jane Doe",
		Project: "Demo",
		Images:  []Image{{Image: "ghcr.io/acme/checkout", Tag: "v1.2.3"}},
	}

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "empty query matches every task", query: "", want: true},
		{name: "app substring, case-insensitive", query: "checkout-a", want: true},
		{name: "author substring", query: "jane", want: true},
		{name: "image name substring", query: "acme/check", want: true},
		{name: "image tag substring", query: "v1.2", want: true},
		{name: "image and tag joined by a colon", query: "checkout:v1.2.3", want: true},
		{name: "no match", query: "payments", want: false},
		{name: "project is not searched", query: "Demo", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, task.MatchesSearch(tt.query))
		})
	}
}

func TestTaskMatchesSearchWithoutImages(t *testing.T) {
	task := Task{App: "api", Author: "bot"}
	assert.False(t, task.MatchesSearch("v1"))
	assert.True(t, task.MatchesSearch("API"))
}
