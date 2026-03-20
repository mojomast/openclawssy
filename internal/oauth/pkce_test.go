package oauth

import (
	"regexp"
	"testing"
)

var urlSafeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestGenerateVerifier_Length(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error: %v", err)
	}
	if len(v) != 43 {
		t.Errorf("verifier length = %d, want 43", len(v))
	}
}

func TestGenerateVerifier_URLSafe(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error: %v", err)
	}
	if !urlSafeRe.MatchString(v) {
		t.Errorf("verifier %q contains non-URL-safe characters", v)
	}
}

func TestChallengeFromVerifier_Deterministic(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error: %v", err)
	}
	c1 := ChallengeFromVerifier(v)
	c2 := ChallengeFromVerifier(v)
	if c1 != c2 {
		t.Errorf("challenge is not deterministic: %q != %q", c1, c2)
	}
}

func TestChallengeFromVerifier_DiffersForDifferentVerifiers(t *testing.T) {
	v1, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error: %v", err)
	}
	v2, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error: %v", err)
	}
	if v1 == v2 {
		t.Skip("extremely unlikely: two identical verifiers generated")
	}
	c1 := ChallengeFromVerifier(v1)
	c2 := ChallengeFromVerifier(v2)
	if c1 == c2 {
		t.Errorf("different verifiers produced the same challenge")
	}
}

func TestChallengeFromVerifier_URLSafe(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error: %v", err)
	}
	c := ChallengeFromVerifier(v)
	if !urlSafeRe.MatchString(c) {
		t.Errorf("challenge %q contains non-URL-safe characters", c)
	}
}
