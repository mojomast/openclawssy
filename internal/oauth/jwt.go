package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// DecodeJWTPayload base64url-decodes the payload segment of a JWT (the middle
// of the three dot-separated parts) and unmarshals it as JSON.
// It is intended only for extracting informational claims, NOT for
// authentication or authorization decisions.
func DecodeJWTPayload(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("oauth/jwt: expected 3 segments, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("oauth/jwt: base64url decode payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("oauth/jwt: unmarshal payload: %w", err)
	}
	return claims, nil
}

// ExtractCodexAccountID decodes the JWT payload and returns the value of the
// "chatgpt_account_id" or "account_id" claim as a string.
// Returns an empty string on any failure or if neither claim is present.
func ExtractCodexAccountID(token string) string {
	claims, err := DecodeJWTPayload(token)
	if err != nil {
		return ""
	}
	for _, key := range []string{"chatgpt_account_id", "account_id"} {
		if v, ok := claims[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
