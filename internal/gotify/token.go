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

// tokenTotalLen is the full token length: type prefix plus the random suffix.
const tokenTotalLen = 1 + tokenLength

// ValidTokenFormat reports whether token has the expected type prefix, length
// and character set. It lets callers cheaply reject malformed tokens before
// doing any datastore lookup, which blunts cost-amplification from clients that
// spam random tokens at the public API.
func ValidTokenFormat(token string, prefix byte) bool {
	if len(token) != tokenTotalLen || token[0] != prefix {
		return false
	}
	for i := 1; i < len(token); i++ {
		switch c := token[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
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
