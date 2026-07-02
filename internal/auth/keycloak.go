package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type KeycloakResponse struct {
	Username string   `json:"preferred_username"`
	Groups   []string `json:"groups"`
}

type KeycloakAuthService struct {
	Url              string
	Realm            string
	ClientId         string
	PrivilegedGroups []string
	client           *http.Client
	userinfoURL      string
}

// Init initializes KeycloakAuthService with Keycloak URL, realm and client ID.
// It validates the base URL and pre-builds the userinfo endpoint URL to prevent SSRF via tainted input.
func (k *KeycloakAuthService) Init(keycloakURL, realm, clientId string, privilegedGroups []string) error {
	baseURL, err := url.Parse(keycloakURL)
	if err != nil {
		return fmt.Errorf("invalid keycloak URL: %w", err)
	}

	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("invalid keycloak URL scheme: %s (must be http or https)", baseURL.Scheme)
	}

	if baseURL.Host == "" {
		return fmt.Errorf("invalid keycloak URL: missing host")
	}

	if baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" {
		return fmt.Errorf("invalid keycloak URL: query and fragment are not allowed")
	}

	userinfoURL := fmt.Sprintf(
		"%s/realms/%s/protocol/openid-connect/userinfo",
		strings.TrimRight(baseURL.String(), "/"),
		url.PathEscape(realm),
	)

	k.Url = keycloakURL
	k.Realm = realm
	k.ClientId = clientId
	k.PrivilegedGroups = privilegedGroups
	k.userinfoURL = userinfoURL
	k.client = &http.Client{Timeout: 10 * time.Second}

	return nil
}

// Validate implements quite simple token validation approach
// We just call Keycloak userinfo endpoint and check if it returns 200
// effectively delegating token validation to Keycloak
func (k *KeycloakAuthService) Validate(token string) (bool, error) {
	var keycloakResponse KeycloakResponse

	req, err := http.NewRequest("GET", k.userinfoURL, nil) // #nosec G704 - URL is validated in Init()
	if err != nil {
		// Transport/internal failure details (URLs, hostnames) stay in the
		// server log; the public-facing error must not leak them.
		log.Error().Err(err).Msg("keycloak: error creating userinfo request")
		return false, errors.New("token validation failed")
	}
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := k.client.Do(req) // #nosec G704 - URL is validated in Init()
	if err != nil {
		log.Error().Err(err).Msg("keycloak: userinfo request failed")
		return false, errors.New("token validation failed")
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msgf("error closing response body: %v", err)
		}
	}(resp.Body)

	// Read and parse the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("keycloak: error reading userinfo response body")
		return false, errors.New("token validation failed")
	}

	if err := json.Unmarshal(bodyBytes, &keycloakResponse); err != nil {
		log.Error().Err(err).Msg("keycloak: error unmarshalling userinfo response")
		return false, errors.New("token validation failed")
	}

	userPrivileged := k.allowedToRollback(keycloakResponse.Username, keycloakResponse.Groups)

	if resp.StatusCode == http.StatusOK && userPrivileged {
		return true, nil
	} else if resp.StatusCode == http.StatusOK && !userPrivileged {
		return false, fmt.Errorf("%s is not a member of any of the privileged groups", keycloakResponse.Username)
	}

	return false, fmt.Errorf("token validation failed with status: %v", resp.Status)
}

// allowedToRollback checks if the user is a member of any of the privileged groups.
// It duplicates the logic from frontend just to be sure that the user did not generate the request manually.
func (k *KeycloakAuthService) allowedToRollback(username string, groups []string) bool {
	for _, group := range groups {
		if slices.Contains(k.PrivilegedGroups, group) {
			log.Debug().Msgf("%s is a member of the privileged group: %v", username, group)
			return true
		}
	}

	log.Debug().Msgf("%s is not a member of any of the privileged groups: %v", username, k.PrivilegedGroups)
	return false
}
