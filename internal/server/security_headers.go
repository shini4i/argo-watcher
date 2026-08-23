package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/shini4i/argo-watcher/internal/config"
)

// cspHead is the invariant part of the Content-Security-Policy, ending on
// connect-src so a request's own websocket sources can be appended. Directives
// looser than 'self' are commented where they are set.
const cspHead = "default-src 'self'; " +
	"script-src 'self'; " +
	// emotion (MUI) and swagger-ui inject stylesheets and style attributes, and a
	// nonce cannot cover the attributes. Google Fonts is loaded by web/index.html.
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	// An avatar comes from the OIDC `picture` claim or Gravatar, so its host is not
	// known here; swagger-ui's stylesheet embeds data: images.
	"img-src 'self' data: https:; " +
	// A provider may serve its token and userinfo endpoints from hosts other than
	// its issuer (Google does), and a policy that breaks sign-in is the worse trade.
	// securityHeaders appends the issuer origin and the websocket sources.
	"connect-src 'self' https:"

// cspTail closes the policy. frame-ancestors is 'self' rather than 'none' because a
// silent renewal without a refresh token falls back to an iframe of this
// application's own origin (oidc-client-ts signinSilent).
const cspTail = "; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'"

// securityHeaders returns middleware that sets the browser-facing response headers
// on every route. It sets them before delegating, so a response written further down
// the chain — the CORS gate's 403, the panic recoverer's 500 — carries them too.
func (env *Env) securityHeaders() func(http.Handler) http.Handler {
	// Only the websocket sources depend on the request; the issuer is fixed for the
	// process lifetime.
	issuer := ""
	if origin := issuerOrigin(env.config.OIDC); origin != "" {
		issuer = " " + origin
	}
	// An issuer on plain http matches neither 'self' nor https:, and the browser
	// fetches discovery, the token exchange and userinfo from it.
	head := cspHead + issuer
	// frame-src takes any https origin because the silent-renewal iframe navigates to
	// the DISCOVERED authorization endpoint, which a provider may host off the issuer
	// origin (Amazon Cognito serves it from the managed-login domain).
	tail := "; frame-src 'self' https:" + issuer + cspTail

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			header.Set("Content-Security-Policy", head+webSocketSources(r.Host)+tail)
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "SAMEORIGIN")
			header.Set("Referrer-Policy", "strict-origin-when-cross-origin")

			next.ServeHTTP(w, r)
		})
	}
}

// webSocketSources returns the connect-src entries for the /ws handshake against
// host, or an empty string when host cannot be spelled in a policy. They are named
// explicitly because connect-src 'self' does not resolve to a websocket scheme in
// every browser (w3c/webappsec-csp#7).
func webSocketSources(host string) string {
	if !isPolicySafeHost(host) {
		slog.Debug("omitting websocket sources from the content security policy", "host", host)
		return ""
	}

	return " ws://" + host + " wss://" + host
}

// issuerOrigin returns the scheme and host of the configured OIDC issuer, which a
// silent renewal iframe navigates to. It is empty when OIDC is off or the issuer is
// not a usable absolute URL.
func issuerOrigin(oidc config.OIDCConfig) string {
	if !oidc.Enabled {
		return ""
	}

	issuer, err := url.Parse(strings.TrimSpace(oidc.IssuerURL))
	if err != nil || (issuer.Scheme != "http" && issuer.Scheme != "https") || !isPolicySafeHost(issuer.Host) {
		slog.Warn("OIDC issuer is not a usable origin; leaving it out of the content security policy",
			"issuer_url", oidc.IssuerURL)
		return ""
	}

	return issuer.Scheme + "://" + issuer.Host
}

// isPolicySafeHost reports whether host is a non-empty host[:port] built only from
// characters that carry no meaning in a policy. Both callers need it: net/http
// accepts ";" and "'" in a Host header and url.Parse accepts them in an issuer, so
// an unvalidated host could append directives to the policy this server serves.
func isPolicySafeHost(host string) bool {
	if host == "" {
		return false
	}

	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return false
		}
	}

	return true
}
