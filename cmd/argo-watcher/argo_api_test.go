package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	mux    = http.NewServeMux()
	server *httptest.Server
	api    *ArgoApi

	userinfo = models.Userinfo{
		Username: "test",
		LoggedIn: true,
	}
)

func getUserInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, err := w.Write([]byte(`Method not allowed`))
		if err != nil {
			fmt.Println(err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(userinfo); err != nil {
		fmt.Println("error encoding userinfo")
		panic(err)
	}
}

func getApplicationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, err := w.Write([]byte(`Method not allowed`))
		if err != nil {
			fmt.Println(err)
		}
		return
	}

	var application models.Application
	application.Status.Health.Status = "Healthy"
	application.Status.Sync.Status = "Synced"
	application.Status.Summary.Images = []string{"example.com/image:v0.1.0", "example.com/image:v0.1.1"}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(application); err != nil {
		fmt.Println("error encoding application")
		panic(err)
	}
}

func init() {
	mux.HandleFunc("/api/v1/session/userinfo", getUserInfoHandler)
	mux.HandleFunc("/api/v1/applications/test", getApplicationHandler)
	server = httptest.NewServer(mux)
	api = &ArgoApi{baseUrl: server.URL, client: server.Client()}
}

func TestArgoApi_GetUserInfo(t *testing.T) {
	receivedUserinfo, err := api.GetUserInfo()
	if err != nil {
		t.Error(err)
	}

	if !reflect.DeepEqual(receivedUserinfo, &userinfo) {
		t.Errorf("Expected: %v, received: %v", userinfo, receivedUserinfo)
	}
}

func TestArgoApi_GetApplication(t *testing.T) {
	app, err := api.GetApplication("test")
	if err != nil {
		t.Error(err)
	}

	if !assert.Equal(t, app.Status.Health.Status, "Healthy") {
		t.Errorf("Expected: %v, received: %v", "Healthy", app.Status.Health.Status)
	}

	if !assert.Equal(t, app.Status.Sync.Status, "Synced") {
		t.Errorf("Expected: %v, received: %v", "Synced", app.Status.Sync.Status)
	}

	if !assert.Equal(t, app.Status.Summary.Images, []string{"example.com/image:v0.1.0", "example.com/image:v0.1.1"}) {
		t.Errorf("Expected: %v, received: %v", []string{"example.com/image:v0.1.0", "example.com/image:v0.1.1"}, app.Status.Summary.Images)
	}
}
