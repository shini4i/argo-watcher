package auth

import (
	"errors"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shini4i/argo-watcher/internal/apptoken"
)

// allowedAppsClaim optionally confines a token to a set of applications. It is a self-restriction,
// not an authorization decision: anyone holding JWT_SECRET can name any application, so it limits
// the blast radius of a leaked *minted token* only. A token omitting both scope claims, or
// listing allowedAppsWildcard, authorizes every application.
const allowedAppsClaim = "allowed_apps"

// allowedAppsWildcard is the allowedAppsClaim entry that covers every application.
// It matches only as a whole entry, never as a glob, and cannot collide with a real
// application because Kubernetes names never contain "*".
const allowedAppsWildcard = "*"

// allowedAppPrefixesClaim optionally confines a token to the applications whose names
// start with one of its entries, under the same self-restriction caveat. Each entry
// must pass apptoken.ValidatePrefix. Alongside allowedAppsClaim, either one matching
// is enough.
const allowedAppPrefixesClaim = "allowed_app_prefixes"

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

// ValidateForApp verifies the token and additionally honors allowedAppsClaim and
// allowedAppPrefixesClaim. A token carrying neither authorizes every application;
// one carrying either is confined to what they name together.
func (j *JWTAuthService) ValidateForApp(tokenStr, app string) (bool, error) {
	token, err := j.parse(tokenStr)
	if err != nil {
		return false, err
	}

	apps, appsPresent, err := stringListClaim(token, allowedAppsClaim)
	if err != nil {
		return false, err
	}

	prefixes, prefixesPresent, err := stringListClaim(token, allowedAppPrefixesClaim)
	if err != nil {
		return false, err
	}

	if !appsPresent && !prefixesPresent {
		return true, nil
	}

	// A malformed prefix is refused rather than skipped: "" or "-" would match nearly
	// every application, widening a token its minter meant to confine.
	for _, prefix := range prefixes {
		if err := apptoken.ValidatePrefix(prefix); err != nil {
			return false, fmt.Errorf("the %s claim is invalid: %w", allowedAppPrefixesClaim, err)
		}
	}

	scope := apptoken.Scope{
		Apps:        apps,
		AppPrefixes: prefixes,
		AllApps:     slices.Contains(apps, allowedAppsWildcard),
	}
	if !scope.Allows(app) {
		return false, fmt.Errorf("the scope in this token's %s and %s claims does not cover application %s",
			allowedAppsClaim, allowedAppPrefixesClaim, app)
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

// stringListClaim reads a claim holding a list of strings, reporting whether the
// token carries it at all. A claim present but malformed is reported rather than
// ignored: treating it as absent would widen the token to the whole estate.
func stringListClaim(token *jwt.Token, name string) (values []string, present bool, err error) {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, false, nil
	}

	raw, present := claims[name]
	if !present {
		return nil, false, nil
	}

	entries, ok := raw.([]any)
	if !ok {
		return nil, true, fmt.Errorf("the %s claim must be an array of strings", name)
	}

	values = make([]string, 0, len(entries))
	for _, entry := range entries {
		value, ok := entry.(string)
		if !ok {
			return nil, true, fmt.Errorf("the %s claim must contain only strings", name)
		}
		values = append(values, value)
	}

	return values, true, nil
}
