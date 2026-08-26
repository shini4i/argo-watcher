package auth

import (
	"errors"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

// allowedAppsClaim optionally confines a token to a set of applications. It is a
// self-restriction, not an authorization decision: anyone holding JWT_SECRET can
// name any application, so it limits the blast radius of a leaked *minted token*
// only. A token omitting the claim keeps authorizing every application.
const allowedAppsClaim = "allowed_apps"

// JWTAuthService validates the HMAC-signed tokens a CI pipeline presents. Beyond the
// signature it enforces the claim policy assembled in NewJWTAuthService.
type JWTAuthService struct {
	secretKey []byte
	options   []jwt.ParserOption
}

// Validate verifies a JSON Web Token, checking signature and claims. It says
// nothing about which applications the token covers — see ValidateForApp.
func (j *JWTAuthService) Validate(tokenStr string) (bool, error) {
	if _, err := j.parse(tokenStr); err != nil {
		return false, err
	}

	return true, nil
}

// ValidateForApp verifies the token and additionally honors allowedAppsClaim,
// confining the token to the applications it names.
func (j *JWTAuthService) ValidateForApp(tokenStr, app string) (bool, error) {
	token, err := j.parse(tokenStr)
	if err != nil {
		return false, err
	}

	allowed, present, err := allowedApps(token)
	if err != nil {
		return false, err
	}

	if !present {
		return true, nil
	}

	if !slices.Contains(allowed, app) {
		return false, fmt.Errorf("this token's %s claim does not cover application %s", allowedAppsClaim, app)
	}

	return true, nil
}

// parse verifies the token's signature and claim policy, returning it for callers
// that need to read further claims.
func (j *JWTAuthService) parse(tokenStr string) (*jwt.Token, error) {
	if tokenStr == "" {
		return nil, fmt.Errorf("empty token")
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	}, j.options...)

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return token, nil
}

// allowedApps reads allowedAppsClaim, reporting whether the token carries it at
// all. A claim present but malformed is reported rather than ignored: treating it
// as absent would widen the token to the whole estate.
func allowedApps(token *jwt.Token) (apps []string, present bool, err error) {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, false, nil
	}

	raw, present := claims[allowedAppsClaim]
	if !present {
		return nil, false, nil
	}

	entries, ok := raw.([]any)
	if !ok {
		return nil, true, fmt.Errorf("the %s claim must be an array of application names", allowedAppsClaim)
	}

	apps = make([]string, 0, len(entries))
	for _, entry := range entries {
		app, ok := entry.(string)
		if !ok {
			return nil, true, fmt.Errorf("the %s claim must contain only application names", allowedAppsClaim)
		}
		apps = append(apps, app)
	}

	return apps, true, nil
}
