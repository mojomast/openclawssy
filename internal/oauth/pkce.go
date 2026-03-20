package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GenerateVerifier generates a PKCE code verifier: 32 random bytes encoded as
// base64url without padding, producing a 43-character URL-safe string.
func GenerateVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ChallengeFromVerifier returns the PKCE S256 code challenge for the given
// verifier: base64url-no-pad encoding of SHA-256(verifier).
func ChallengeFromVerifier(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
