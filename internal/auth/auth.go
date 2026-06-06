// Package auth holds stateless helpers for extracting Gotify tokens and HTTP
// Basic credentials from a request. Resolving those against the store and
// applying policy lives in the API handler.
package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

// ExtractToken pulls a Gotify access token from (in order): the "token" query
// parameter, the X-Gotify-Key header, or an "Authorization: Bearer" header.
// header keys are expected to be lower-cased (API Gateway HTTP API v2 does this).
func ExtractToken(headers, query map[string]string) string {
	if t := query["token"]; t != "" {
		return t
	}
	if t := headers["x-gotify-key"]; t != "" {
		return t
	}
	if a := headers["authorization"]; strings.HasPrefix(a, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(a, "Bearer "))
	}
	return ""
}

// BasicAuth decodes an HTTP Basic Authorization header.
func BasicAuth(headers map[string]string) (user, pass string, ok bool) {
	a := headers["authorization"]
	if !strings.HasPrefix(a, "Basic ") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(a, "Basic "))
	if err != nil {
		return "", "", false
	}
	user, pass, found := strings.Cut(string(raw), ":")
	if !found {
		return "", "", false
	}
	return user, pass, true
}

// Equal is a constant-time string comparison.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
