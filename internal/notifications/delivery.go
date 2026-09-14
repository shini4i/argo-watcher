package notifications

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	// maxErrorBodySize caps how much of a rejected delivery's response body is
	// quoted back in the error.
	maxErrorBodySize = 2 * 1024 // 2 KB
	// maxResponseBodySize caps how much of an accepted delivery's response body is
	// read. Only Mattermost reads one back, and a receiver is not trusted to keep
	// it small.
	maxResponseBodySize = 1 << 20 // 1 MiB
	// notificationTimeout bounds one delivery, so an unresponsive receiver cannot
	// hold a rollout goroutine open.
	notificationTimeout = 30 * time.Second
)

// validateReceiverURL rejects a receiver URL no delivery could ever reach: blank, or
// not an absolute http(s) URL. It returns the trimmed URL, which is what callers must
// store — validating one value and sending another reinstates the per-send failure
// this refuses at construction. The URL is never quoted: it is itself a credential.
func validateReceiverURL(service, rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("%s url cannot be empty", service)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%s url is not a valid URL", service)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s url must be an http:// or https:// URL", service)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%s url is missing a host", service)
	}

	return trimmed, nil
}

// deliverPost posts body to rawURL and returns the response body, read up to
// maxResponseBodySize. A status outside allowed is an error quoting the response
// body, which is what says why the receiver refused. service names the receiver in
// every error; none quotes rawURL (see validateReceiverURL and redactURL).
func deliverPost(client HTTPClient, service, rawURL string, headers map[string]string, body []byte, allowed []int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create %s request: %w", service, redactURL(err))
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s request: %w", service, redactURL(err))
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Failed to close notification response body", "service", service, "error", err)
		}
	}()

	if !slices.Contains(allowed, resp.StatusCode) {
		errBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		if readErr != nil {
			return nil, fmt.Errorf("%s returned status code %d, and failed to read the response body: %w", service, resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("%s returned status code %d: %s", service, resp.StatusCode, errBody)
	}

	accepted, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read the %s response body: %w", service, err)
	}

	return accepted, nil
}
