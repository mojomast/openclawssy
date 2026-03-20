package oauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// makeJWT builds a minimal, unsigned JWT with the given payload claims.
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("makeJWT: marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return strings.Join([]string{header, payload, ""}, ".")
}

func TestExtractCodexAccountID_AccountID(t *testing.T) {
	token := makeJWT(t, map[string]any{"account_id": "acct_abc123", "sub": "user1"})
	got := ExtractCodexAccountID(token)
	if got != "acct_abc123" {
		t.Errorf("got %q, want %q", got, "acct_abc123")
	}
}

func TestExtractCodexAccountID_ChatGPTAccountID(t *testing.T) {
	token := makeJWT(t, map[string]any{"chatgpt_account_id": "cgpt_xyz789", "sub": "user2"})
	got := ExtractCodexAccountID(token)
	if got != "cgpt_xyz789" {
		t.Errorf("got %q, want %q", got, "cgpt_xyz789")
	}
}

func TestExtractCodexAccountID_ChatGPTPreferredOverAccountID(t *testing.T) {
	// chatgpt_account_id is checked first.
	token := makeJWT(t, map[string]any{
		"chatgpt_account_id": "cgpt_first",
		"account_id":         "acct_second",
	})
	got := ExtractCodexAccountID(token)
	if got != "cgpt_first" {
		t.Errorf("got %q, want %q", got, "cgpt_first")
	}
}

func TestExtractCodexAccountID_MalformedToken(t *testing.T) {
	got := ExtractCodexAccountID("not.a.valid.jwt.at.all!!!")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExtractCodexAccountID_TwoSegments(t *testing.T) {
	got := ExtractCodexAccountID("header.payload")
	if got != "" {
		t.Errorf("got %q, want empty string for 2-segment token", got)
	}
}

func TestExtractCodexAccountID_EmptyToken(t *testing.T) {
	got := ExtractCodexAccountID("")
	if got != "" {
		t.Errorf("got %q, want empty string for empty token", got)
	}
}
