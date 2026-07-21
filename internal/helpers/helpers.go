package helpers

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"slices"
	"strings"

	"crypto/sha256"
)

// ImagesContains reports whether images contains image. When a registry proxy is
// set it matches the image both with and without the proxy prefix.
func ImagesContains(images []string, image string, registryProxy string) bool {
	if registryProxy != "" {
		imageWithProxy := registryProxy + "/" + image
		// We need to check image with and without proxy because mutating webhook
		// might not have finished image copy during first rollout part. (due to 30s timeout)
		return slices.Contains(images, image) || slices.Contains(images, imageWithProxy)
	} else {
		return slices.Contains(images, image)
	}
}

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
		// Skip past the request line and headers to the body.
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

// NormalizeImages returns a sorted copy of the provided image slice to guarantee stable ordering without mutating the original.
func NormalizeImages(images []string) []string {
	copied := append([]string(nil), images...)
	slices.Sort(copied)
	return copied
}
