package argocd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rs/zerolog/log"

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
		log.Error().Msg("error encoding userinfo")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
		log.Error().Msg("error encoding application")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func init() {
	mux.HandleFunc("/api/v1/session/userinfo", getUserInfoHandler)
	mux.HandleFunc("/api/v1/applications/test", getApplicationHandler)
	server = httptest.NewServer(mux)
	parsedURL, _ := url.Parse(server.URL) // we assume that the server.URL is valid
	api = &ArgoApi{baseUrl: *parsedURL, client: server.Client()}
}

func TestArgoApi_GetUserInfo(t *testing.T) {
	if receivedUserinfo, err := api.GetUserInfo(); err != nil {
		t.Error(err)
	} else {
		assert.Equal(t, userinfo, *receivedUserinfo)
	}
}

func TestArgoApi_GetApplication(t *testing.T) {
	if app, err := api.GetApplication("test"); err != nil {
		t.Error(err)
	} else {
		assert.Equal(t, "Healthy", app.Status.Health.Status)
		assert.Equal(t, "Synced", app.Status.Sync.Status)
		assert.Equal(t, []string{"example.com/image:v0.1.0", "example.com/image:v0.1.1"}, app.Status.Summary.Images)
	}
}
