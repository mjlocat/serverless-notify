package gotify

import (
	"crypto/rand"
	"encoding/base64"
)

// tokenLength is the number of random characters following the type prefix,
// matching upstream Gotify (total token length is 15: 1 prefix + 14 chars).
const tokenLength = 14

// GenerateApplicationToken returns a Gotify-compatible application token
// (prefix "A").
func GenerateApplicationToken() string {
	return generateToken('A')
}

// GenerateClientToken returns a Gotify-compatible client token (prefix "C").
func GenerateClientToken() string {
	return generateToken('C')
}

// generateToken mirrors Gotify's token generation: a type prefix followed by
// URL-safe base64 random characters. The alphabet (A-Za-z0-9-_) and length
// match the upstream server so existing clients accept the tokens.
func generateToken(prefix byte) string {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return string(prefix) + base64.RawURLEncoding.EncodeToString(b)[:tokenLength]
}
