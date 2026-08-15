package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// TestWriteJSONUnmarshalableBody covers why writeJSON marshals before it touches the
// writer: an unencodable value must produce an error status, not a truncated 200.
func TestWriteJSONUnmarshalableBody(t *testing.T) {
	w := httptest.NewRecorder()

	writeJSON(w, http.StatusOK, make(chan int))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, w.Body.String())
}

// TestBindJSONRejectsAbsentBody pins the wording of the error a request with no body
// produces, which released clients may already be matching on.
func TestBindJSONRejectsAbsentBody(t *testing.T) {
	var task models.Task

	req, err := http.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	require.NoError(t, err)

	err = bindJSON(req, &task)

	require.Error(t, err)
	assert.Equal(t, "invalid request", err.Error())
}
