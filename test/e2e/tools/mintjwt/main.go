// Command mintjwt prints a short-lived HS256 JSON Web Token signed with the
// value of the JWT_SECRET environment variable. It exists so the e2e lab can
// exercise argo-watcher's BEARER_TOKEN auth path without depending on an external
// tool (openssl is not guaranteed on the lab host); it signs with the very
// library the server validates with (golang-jwt/jwt/v5), so the token is
// guaranteed compatible. The token carries the iat and exp claims the server
// requires and is valid for one hour — long enough for a single deploy phase.
// JWT_ISS and JWT_AUD add the iss and aud claims a server configured with
// JWT_ISSUER / JWT_AUDIENCE requires.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "mintjwt: JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}

	// Set only when asked for, so the same tool mints both the claimless token an
	// unconfigured server accepts and the bound one JWT_ISSUER/JWT_AUDIENCE demand.
	if issuer := os.Getenv("JWT_ISS"); issuer != "" {
		claims["iss"] = issuer
	}
	if audience := os.Getenv("JWT_AUD"); audience != "" {
		claims["aud"] = audience
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		fmt.Fprintln(os.Stderr, "mintjwt: failed to sign token:", err)
		os.Exit(1)
	}

	// No trailing newline: the caller captures this directly into an env var.
	fmt.Print(signed)
}
