package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// userInfoResponse holds the subset of OIDC userinfo claims argo-watcher needs.
// preferred_username identifies the user in logs; groups drives the privileged
// rollback check. Both are standard OIDC claims the provider must expose in its
// userinfo response — Keycloak and Authentik both support mapping them.
type userInfoResponse struct {
	Username string   `json:"preferred_username"`
	Groups   []string `json:"groups"`
}

// discoveryDocument is the subset of the OIDC discovery metadata we consume.
type discoveryDocument struct {
	UserinfoEndpoint string `json:"userinfo_endpoint"`
}

// OIDCAuthService validates bearer tokens against any OIDC-compliant provider by
// calling the provider's userinfo endpoint. The endpoint is resolved lazily on
// first use via OIDC discovery, so process startup never depends on the provider
// being reachable.
type OIDCAuthService struct {
	IssuerURL        string
	ClientId         string
	PrivilegedGroups []string
	client           *http.Client

	mu          sync.Mutex
	userinfoURL string
}

// Init validates the issuer URL and stores the service configuration. It does
// not contact the network: the userinfo endpoint is discovered lazily on the
// first Validate call (see resolveUserinfoURL), preserving the pre-refactor
// behaviour where startup never reached out to the identity provider.
func (o *OIDCAuthService) Init(issuerURL, clientId string, privilegedGroups []string) error {
	if err := validateIssuerURL(issuerURL); err != nil {
		return err
	}

	o.IssuerURL = issuerURL
	o.ClientId = clientId
	o.PrivilegedGroups = privilegedGroups
	o.client = &http.Client{Timeout: 10 * time.Second}

	return nil
}

// validateIssuerURL rejects issuer URLs that are unusable or unsafe to build
// discovery requests from (non-http(s) scheme, missing host, or carrying a query
// or fragment), guarding against SSRF via tainted configuration.
func validateIssuerURL(issuerURL string) error {
	baseURL, err := url.Parse(issuerURL)
	if err != nil {
		return fmt.Errorf("invalid OIDC issuer URL: %w", err)
	}

	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("invalid OIDC issuer URL scheme: %s (must be http or https)", baseURL.Scheme)
	}

	if baseURL.Host == "" {
		return fmt.Errorf("invalid OIDC issuer URL: missing host")
	}

	if baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" {
		return fmt.Errorf("invalid OIDC issuer URL: query and fragment are not allowed")
	}

	return nil
}

// resolveUserinfoURL returns the provider's userinfo endpoint, discovering it
// once via {issuer}/.well-known/openid-configuration and caching the result. It
// is safe for concurrent use.
//
// A discovery failure is returned to the caller and NOT cached, so the next
// request retries: a transient provider outage must not permanently disable
// authentication. The discovered endpoint is re-validated (scheme + host) before
// use to keep the SSRF guarantee even though the value comes from the issuer.
func (o *OIDCAuthService) resolveUserinfoURL() (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.userinfoURL != "" {
		return o.userinfoURL, nil
	}

	discoveryURL := strings.TrimRight(o.IssuerURL, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequest(http.MethodGet, discoveryURL, nil) // #nosec G704 - issuer URL is validated in Init()
	if err != nil {
		slog.Error("oidc: error creating discovery request", "error", err)
		return "", errors.New("token validation failed")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req) // #nosec G704 - issuer URL is validated in Init()
	if err != nil {
		slog.Error("oidc: discovery request failed", "issuer", o.IssuerURL, "error", err)
		return "", errors.New("token validation failed")
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Error("oidc: discovery returned non-200", "issuer", o.IssuerURL, "status", resp.Status)
		return "", errors.New("token validation failed")
	}

	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		slog.Error("oidc: error decoding discovery document", "error", err)
		return "", errors.New("token validation failed")
	}

	if err := validateUserinfoURL(doc.UserinfoEndpoint); err != nil {
		slog.Error("oidc: invalid userinfo endpoint in discovery document", "error", err)
		return "", errors.New("token validation failed")
	}

	o.userinfoURL = doc.UserinfoEndpoint
	return o.userinfoURL, nil
}

// validateUserinfoURL guards the endpoint advertised by the discovery document
// so a malformed or non-http(s) value cannot be turned into an SSRF request.
func validateUserinfoURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("discovery document is missing userinfo_endpoint")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("unparseable userinfo endpoint: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid userinfo endpoint scheme: %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return errors.New("userinfo endpoint is missing host")
	}

	return nil
}

// Validate implements a simple token validation approach: it calls the OIDC
// provider's userinfo endpoint with the bearer token and treats HTTP 200 as
// proof the token is valid, effectively delegating validation to the provider.
// The user must additionally belong to a privileged group to be authorized.
func (o *OIDCAuthService) Validate(token string) (bool, error) {
	userinfoURL, err := o.resolveUserinfoURL()
	if err != nil {
		return false, err
	}

	var info userInfoResponse

	req, err := http.NewRequest(http.MethodGet, userinfoURL, nil) // #nosec G704 - URL is validated in resolveUserinfoURL()
	if err != nil {
		// Transport/internal failure details (URLs, hostnames) stay in the
		// server log; the public-facing error must not leak them.
		slog.Error("oidc: error creating userinfo request", "error", err)
		return false, errors.New("token validation failed")
	}
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := o.client.Do(req) // #nosec G704 - URL is validated in resolveUserinfoURL()
	if err != nil {
		slog.Error("oidc: userinfo request failed", "error", err)
		return false, errors.New("token validation failed")
	}
	defer closeBody(resp.Body)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("oidc: error reading userinfo response body", "error", err)
		return false, errors.New("token validation failed")
	}

	if err := json.Unmarshal(bodyBytes, &info); err != nil {
		slog.Error("oidc: error unmarshalling userinfo response", "error", err)
		return false, errors.New("token validation failed")
	}

	userPrivileged := o.allowedToRollback(info.Username, info.Groups)

	if resp.StatusCode == http.StatusOK && userPrivileged {
		return true, nil
	} else if resp.StatusCode == http.StatusOK && !userPrivileged {
		return false, fmt.Errorf("%s is not a member of any of the privileged groups", info.Username)
	}

	return false, fmt.Errorf("token validation failed with status: %v", resp.Status)
}

// allowedToRollback checks if the user is a member of any of the privileged groups.
// It duplicates the logic from frontend just to be sure that the user did not generate the request manually.
func (o *OIDCAuthService) allowedToRollback(username string, groups []string) bool {
	for _, group := range groups {
		if slices.Contains(o.PrivilegedGroups, group) {
			slog.Debug("user is a member of a privileged group", "username", username, "group", group)
			return true
		}
	}

	slog.Debug("user is not a member of any privileged group", "username", username, "privileged_groups", o.PrivilegedGroups)
	return false
}

// closeBody closes an HTTP response body and logs any error, keeping the call
// sites free of repeated deferred-close boilerplate.
func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		slog.Error("error closing response body", "error", err)
	}
}
