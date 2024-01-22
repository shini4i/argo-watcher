package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/rs/zerolog/log"
)

type KeycloakResponse struct {
	Groups []string `json:"groups"`
}

type KeycloakAuthService struct {
	Url              string
	Realm            string
	ClientId         string
	PrivilegedGroups []string
	client           *http.Client
}

// Init is used to initialize KeycloakAuthService with Keycloak URL, realm and client ID
func (k *KeycloakAuthService) Init(url, realm, clientId string, privilegedGroups []string) {
	k.Url = url
	k.Realm = realm
	k.ClientId = clientId
	k.PrivilegedGroups = privilegedGroups
	k.client = &http.Client{}
}

// Validate implements quite simple token validation approach
// We just call Keycloak userinfo endpoint and check if it returns 200
// effectively delegating token validation to Keycloak
func (k *KeycloakAuthService) Validate(token string) (bool, error) {
	var keycloakResponse KeycloakResponse

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", k.Url, k.Realm), nil)
	if err != nil {
		return false, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := k.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("error on response: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msgf("error closing response body: %v", err)
		}
	}(resp.Body)

	// Read and print the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Msgf("error reading response body: %v", err)
	} else {
		if err := json.Unmarshal(bodyBytes, &keycloakResponse); err != nil {
			log.Error().Msgf("error unmarshalling response body: %v", err)
		}
	}

	userPrivileged := k.allowedToRollback(keycloakResponse.Groups)

	if resp.StatusCode == http.StatusOK && userPrivileged {
		return true, nil
	} else if resp.StatusCode == http.StatusOK && !userPrivileged {
		return false, fmt.Errorf("user is not a member of any of the privileged groups")
	}

	return false, fmt.Errorf("token validation failed with status: %v", resp.Status)
}

// allowedToRollback checks if the user is a member of any of the privileged groups
// It duplicates the logic from fronted just to be sure that the user did not generate the request manually
func (k *KeycloakAuthService) allowedToRollback(groups []string) bool {
	for _, group := range groups {
		if helpers.Contains(k.PrivilegedGroups, group) {
			log.Debug().Msgf("User is a member of the privileged group: %v", group)
			return true
		}
	}

	log.Debug().Msgf("User is not a member of any of the privileged groups: %v", k.PrivilegedGroups)
	return false
}
