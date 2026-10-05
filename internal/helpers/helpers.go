package helpers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"

	"crypto/sha256"
)

// CurlCommandFromRequest renders an HTTP request as an equivalent cURL command
// (method, headers, body, URL). The value of any header whose name matches
// redactHeaders (case-insensitively) is replaced with "<redacted>" so secrets
// such as auth tokens are not written to logs; the header name is kept.
func CurlCommandFromRequest(request *http.Request, redactHeaders ...string) (string, error) {
	clonedRequest, err := httputil.DumpRequest(request, true)
	if err != nil {
		return "", err
	}

	cmd := "curl -X " + request.Method

	for key, values := range request.Header {
		if headerMatches(key, redactHeaders) {
			cmd += fmt.Sprintf(" -H '%s: <redacted>'", shellEscapeSingleQuote(key))
			continue
		}
		for _, value := range values {
			cmd += fmt.Sprintf(" -H '%s: %s'", shellEscapeSingleQuote(key), shellEscapeSingleQuote(value))
		}
	}

	if len(clonedRequest) > 0 {
		headerEndIndex := strings.Index(string(clonedRequest), "\r\n\r\n")
		if headerEndIndex != -1 && headerEndIndex+4 <= len(clonedRequest) {
			body := string(clonedRequest[headerEndIndex+4:])
			if len(body) > 0 {
				cmd += " -d '" + shellEscapeSingleQuote(body) + "'"
			}
		}
	}

	cmd += " '" + shellEscapeSingleQuote(request.URL.String()) + "'"

	return cmd, nil
}

// headerMatches reports whether the given header name matches any entry in
// names, comparing case-insensitively to tolerate HTTP header canonicalization.
func headerMatches(name string, names []string) bool {
	for _, candidate := range names {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}

// shellEscapeSingleQuote escapes single quotes for use inside single-quoted shell strings.
// Each single quote is replaced with the following four-character sequence, which ends the
// current single-quoted string, adds an escaped single quote, and starts a new one:
//
//	'\''
func shellEscapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// GenerateHash returns the SHA-256 digest of s.
func GenerateHash(s string) []byte {
	hash := sha256.New()
	// hash.Write is documented never to return an error.
	hash.Write([]byte(s))
	return hash.Sum(nil)
}

// MaxRedirects mirrors net/http's default redirect limit, which setting CheckRedirect
// replaces.
const MaxRedirects = 10

// ErrInsecureRedirect is returned by RefuseSchemeDowngrade for a hop from https to a weaker
// scheme. Callers treat it as terminal: retrying replays the same redirect.
var ErrInsecureRedirect = errors.New("refused to follow a redirect away from https")

// RefuseSchemeDowngrade is the CheckRedirect core for clients that carry a credential: it stops
// after MaxRedirects hops and wraps ErrInsecureRedirect, naming urlEnvVar, for an https hop to a
// weaker scheme. net/http forwards credentials by hostname alone. Comparing against the hop just
// taken keeps an endpoint configured on http working.
func RefuseSchemeDowngrade(request *http.Request, via []*http.Request, urlEnvVar string) error {
	if len(via) >= MaxRedirects {
		return fmt.Errorf("stopped after %d redirects", MaxRedirects)
	}

	previous := via[len(via)-1]
	if previous.URL.Scheme == "https" && request.URL.Scheme != "https" {
		return fmt.Errorf("%w: %q redirected to %q. Point %s at the https endpoint",
			ErrInsecureRedirect, previous.URL.Host, request.URL.Scheme+"://"+request.URL.Host, urlEnvVar)
	}

	return nil
}
