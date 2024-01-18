package auth

import (
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

type KeycloakAuthService struct {
	Url      string
	Realm    string
	ClientId string
	client   *http.Client
}

// Init is used to initialize KeycloakAuthService with Keycloak URL, realm and client ID
func (k *KeycloakAuthService) Init(url, realm, clientId string) {
	k.Url = url
	k.Realm = realm
	k.ClientId = clientId
	k.client = &http.Client{}
}

// Validate implements quite simple token validation approach
// We just call Keycloak userinfo endpoint and check if it returns 200
// effectively delegating token validation to Keycloak
func (k *KeycloakAuthService) Validate(token string) (bool, error) {
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

	log.Debug().Msgf("Token validation response: %v", resp.Status)
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}

	return false, fmt.Errorf("token validation failed with status: %v", resp.Status)
}
