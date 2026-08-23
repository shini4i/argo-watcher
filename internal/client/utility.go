package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
)

// transientError wraps a failure that is safe to retry: a network-level error
// (connection refused/reset, DNS, timeout) or a 5xx response from argo-watcher.
// Terminal failures (4xx, malformed 200 bodies) are returned unwrapped so the
// retry loop stops immediately.
type transientError struct {
	err error
}

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

// doRequest presents the configured credential on every request it sends.
func (watcher *Watcher) doRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	watcher.auth.apply(req)
	return watcher.client.Do(req)
}

// getJSON sends a GET request to a provided URL, parses the JSON response and
// stores it in the value pointed by v. Transient failures (network errors or
// 5xx responses) are retried up to maxTransientRetries times with a fixed
// backoff, so a temporary networking problem does not abort a long-running
// deployment poll. Terminal failures (4xx, malformed responses) are returned
// immediately — retrying them never succeeds.
func (watcher *Watcher) getJSON(url string, v interface{}) error {
	for attempt := 0; ; attempt++ {
		err := watcher.getJSONOnce(url, v)
		if err == nil {
			return nil
		}

		var te transientError
		if !errors.As(err, &te) || attempt >= maxTransientRetries {
			return err
		}

		log.Printf("transient error talking to argo-watcher (attempt %d/%d): %v; retrying in %s",
			attempt+1, maxTransientRetries, err, watcher.retryDelay)
		time.Sleep(watcher.retryDelay)
	}
}

// getJSONOnce wraps retryable failures in transientError; terminal ones are returned as-is.
func (watcher *Watcher) getJSONOnce(url string, v interface{}) error {
	resp, err := watcher.doRequest(http.MethodGet, url, nil)
	if err != nil {
		if errors.Is(err, errInsecureRedirect) {
			return err // a misconfiguration, not a blip — retrying never clears it
		}
		// Network-level failure: connection refused/reset, DNS, timeout.
		return transientError{err}
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		serverErr := serverErrorFromResponse(resp.StatusCode, body)
		if resp.StatusCode >= http.StatusInternalServerError {
			// 5xx: the server is up but temporarily unavailable (e.g. restarting).
			return transientError{serverErr}
		}
		return serverErr
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

// serverErrorFromResponse builds a human-readable error from an unsuccessful
// HTTP response. It tries to decode the body as a TaskStatus to extract the
// server's `error` field; failing that, it falls back to the raw body text.
// For 401/403 it appends a hint about which env vars govern auth, since the
// most common cause is a missing or wrong token on the client side.
func serverErrorFromResponse(statusCode int, body []byte) error {
	reason := serverReason(body)
	if reason == "" {
		return fmt.Errorf("argo-watcher returned status %d", statusCode)
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return fmt.Errorf("argo-watcher returned status %d: %s "+
			"(check ARGO_WATCHER_DEPLOY_TOKEN or BEARER_TOKEN)", statusCode, reason)
	}
	return fmt.Errorf("argo-watcher returned status %d: %s", statusCode, reason)
}

// serverReason extracts a message from the response body, falling back to the raw body
// when neither the `error` nor the `status` field of models.TaskStatus is present.
func serverReason(body []byte) string {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return ""
	}
	var ts models.TaskStatus
	if err := json.Unmarshal(body, &ts); err == nil {
		if ts.Error != "" {
			return ts.Error
		}
		if ts.Status != "" {
			return ts.Status
		}
	}
	return string(body)
}

func getImagesList(list []string, tag string) []models.Image {
	var images []models.Image
	for _, image := range list {
		images = append(images, models.Image{
			Image: image,
			Tag:   tag,
		})
	}
	return images
}

func createTask(config *Config) models.Task {
	images := getImagesList(config.Images, config.Tag)
	return models.Task{
		App:     config.App,
		Author:  config.Author,
		Project: config.Project,
		Images:  images,
		Timeout: config.TaskTimeout,
		Refresh: config.Refresh,
	}
}

// printClientConfiguration logs the client configuration and warns when no auth token is set.
func printClientConfiguration(watcher *Watcher, task models.Task) {
	fmt.Printf("Got the following configuration:\n"+
		"ARGO_WATCHER_URL: %s\n"+
		"ARGO_APP: %s\n"+
		"COMMIT_AUTHOR: %s\n"+
		"PROJECT_NAME: %s\n"+
		"IMAGE_TAG: %s\n"+
		"IMAGES: %s\n\n",
		watcher.baseUrl, task.App, task.Author, task.Project, clientConfig.Tag, task.Images)
	if clientConfig.Token == "" && clientConfig.JsonWebToken == "" {
		fmt.Println("Neither deploy token nor JSON Web token found, git commit will not be performed")
	}
}

// generateAppUrl builds the ArgoCD UI link for the task's application from the
// server's config, preferring ARGO_URL_ALIAS when the server publishes one.
// It fails when that config cannot be fetched or carries an unparsable URL.
func generateAppUrl(watcher *Watcher, task models.Task) (string, error) {
	cfg, err := watcher.getWatcherConfig()
	if err != nil {
		return "", err
	}

	base := cfg.ArgoUrlAlias
	if base == "" {
		base = cfg.ArgoUrl.String()
	}
	// JoinPath puts the route in the path, where a query or fragment on the base
	// would otherwise swallow it, and folds a trailing slash on the way.
	return url.JoinPath(base, "applications", task.App)
}

// setupWatcher takes application configuration and initializes a new Watcher instance
// with the specified parameters, including the credential it presents on every
// request to argo-watcher.
func setupWatcher(config *Config) *Watcher {
	watcher := NewWatcher(
		strings.TrimSuffix(config.Url, "/"),
		config.Debug,
		config.Timeout,
	)
	watcher.auth = credentialFrom(config)
	return watcher
}
