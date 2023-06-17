package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func init() {
	mux.HandleFunc("/api/v1/session/userinfo", getUserInfoHandler)
	mux.HandleFunc("/api/v1/applications/", getApplicationHandler)
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
	t.Skip("skipping test")
}
