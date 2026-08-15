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

type discoveryDocument struct {
	UserinfoEndpoint string `json:"userinfo_endpoint"`
}

// ErrProviderUnavailable marks a failure to reach the OIDC provider or to make sense
// of its answer, as distinct from a token it actively rejected. Handlers map it to 503,
// because the frontend discards its session on a 401 — a blip would log everyone out.
// Its message is the sanitized text clients see; details stay in the server log.
var ErrProviderUnavailable = errors.New("token validation failed")

// OIDCAuthService validates bearer tokens against any OIDC-compliant provider by
// calling the provider's userinfo endpoint. The endpoint is resolved lazily on
// first use via OIDC discovery, so process startup never depends on the provider
// being reachable.
//
// Decisions are memoized per token (see validationCache) so that authorizing
// every request costs one userinfo round trip per token per validation interval.
type OIDCAuthService struct {
	IssuerURL        string
	ClientId         string
	PrivilegedGroups []string
	client           *http.Client
	cache            *validationCache

	mu          sync.Mutex
	userinfoURL string
}

// Init validates the issuer URL and stores the service configuration. It does
// not contact the network: the userinfo endpoint is discovered lazily on the
// first Validate call (see resolveUserinfoURL), preserving the pre-refactor
// behaviour where startup never reached out to the identity provider.
//
// validationInterval is how long a provider decision may be reused for a given
// token; it bounds how stale an authorization decision can be. Zero disables
// caching, validating every request against the provider.
func (o *OIDCAuthService) Init(issuerURL, clientId string, privilegedGroups []string, validationInterval time.Duration) error {
	if err := validateIssuerURL(issuerURL); err != nil {
		return err
	}

	o.IssuerURL = issuerURL
	o.ClientId = clientId
	o.PrivilegedGroups = privilegedGroups
	o.client = &http.Client{Timeout: 10 * time.Second}
	o.cache = newValidationCache(validationInterval)

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
		return "", ErrProviderUnavailable
	}
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req) // #nosec G704 - issuer URL is validated in Init()
	if err != nil {
		slog.Error("oidc: discovery request failed", "issuer", o.IssuerURL, "error", err)
		return "", ErrProviderUnavailable
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Error("oidc: discovery returned non-200", "issuer", o.IssuerURL, "status", resp.Status)
		return "", ErrProviderUnavailable
	}

	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		slog.Error("oidc: error decoding discovery document", "error", err)
		return "", ErrProviderUnavailable
	}

	if err := validateUserinfoURL(doc.UserinfoEndpoint); err != nil {
		slog.Error("oidc: invalid userinfo endpoint in discovery document", "error", err)
		return "", ErrProviderUnavailable
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

// Authenticate reports whether a token identifies a signed-in user, without
// requiring any privilege. It is the check for read access: every authenticated
// user may read, while privileged actions go through Validate.
//
// It returns nil when the provider accepts the token, an error naming the reason
// when the provider rejects it, and ErrProviderUnavailable when the provider could
// not be consulted at all.
func (o *OIDCAuthService) Authenticate(token string) error {
	_, err := o.resolveIdentity(token, true)
	return err
}

// Validate implements AuthStrategy for privileged operations: it calls the OIDC
// provider's userinfo endpoint with the bearer token and treats HTTP 200 as proof
// the token is valid, effectively delegating validation to the provider. The user
// must additionally belong to a privileged group to be authorized.
//
// It deliberately does not answer from a cached acceptance: a privileged action is a
// rare human click, so re-asking the provider costs nothing and keeps group removal
// and session revocation immediate rather than within OIDC_TOKEN_VALIDATION_INTERVAL.
func (o *OIDCAuthService) Validate(token string) (bool, error) {
	info, err := o.resolveIdentity(token, false)
	if err != nil {
		return false, err
	}

	if !o.allowedToRollback(info.Username, info.Groups) {
		return false, fmt.Errorf("%s is not a member of any of the privileged groups", info.Username)
	}

	return true, nil
}

// resolveIdentity returns the provider's view of a token, recording the outcome for
// later use. With allowCached it answers from the cache; without it, only a cached
// rejection may be reused — a rejection cannot over-grant, so honoring it still blunts
// a hot loop of bad tokens, while reusing an acceptance is what would extend a stale
// privilege.
//
// A provider failure is never cached: it would deny access for the whole interval over
// what may be a momentary blip.
func (o *OIDCAuthService) resolveIdentity(token string, allowCached bool) (*userInfoResponse, error) {
	if entry, ok := o.cache.get(token); ok && (allowCached || entry.err != nil) {
		return entry.info, entry.err
	}

	info, err := o.fetchUserInfo(token)
	if errors.Is(err, ErrProviderUnavailable) {
		return nil, err
	}

	o.cache.put(token, info, err)

	return info, err
}

// fetchUserInfo calls the provider's userinfo endpoint with the given bearer token.
// Failures to reach, parse or make sense of the provider yield ErrProviderUnavailable;
// only a 401/403 is reported as a rejection of the token.
func (o *OIDCAuthService) fetchUserInfo(token string) (*userInfoResponse, error) {
	userinfoURL, err := o.resolveUserinfoURL()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, userinfoURL, nil) // #nosec G704 - URL is validated in resolveUserinfoURL()
	if err != nil {
		// Transport/internal failure details (URLs, hostnames) stay in the
		// server log; the public-facing error must not leak them.
		slog.Error("oidc: error creating userinfo request", "error", err)
		return nil, ErrProviderUnavailable
	}
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := o.client.Do(req) // #nosec G704 - URL is validated in resolveUserinfoURL()
	if err != nil {
		slog.Error("oidc: userinfo request failed", "error", err)
		return nil, ErrProviderUnavailable
	}
	defer closeBody(resp.Body)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("oidc: error reading userinfo response body", "error", err)
		return nil, ErrProviderUnavailable
	}

	// Only 401/403 are the provider judging the token; any other non-200 means it
	// did not judge it at all, and a rejection would sign valid users out.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("token validation failed with status: %v", resp.Status)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("oidc: userinfo returned an unusable status", "status", resp.Status)
		return nil, ErrProviderUnavailable
	}

	var info userInfoResponse
	if err := json.Unmarshal(bodyBytes, &info); err != nil {
		slog.Error("oidc: error unmarshalling userinfo response", "error", err)
		return nil, ErrProviderUnavailable
	}

	return &info, nil
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

func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		slog.Error("error closing response body", "error", err)
	}
}
