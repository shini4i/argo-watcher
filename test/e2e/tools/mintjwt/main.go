// Command mintjwt prints a one-hour HS256 token signed with JWT_SECRET, using the
// very library the server validates with, so the lab needs no openssl. JWT_ISS,
// JWT_AUD and JWT_ALLOWED_APPS (comma-separated) add the iss, aud and allowed_apps
// claims; iat and exp are always set, because the server requires them.
package main

import (
	"fmt"
	"os"
	"strings"
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
	// A list, not a string: the claim is read back as one, and a token omitting it
	// keeps authorizing every application.
	if apps := os.Getenv("JWT_ALLOWED_APPS"); apps != "" {
		claims["allowed_apps"] = strings.Split(apps, ",")
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
