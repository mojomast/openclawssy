package oauth

import (
	"testing"
	"time"
)

func TestPendingStore_CreateAndGet(t *testing.T) {
	ps := NewPendingStore()
	sess := ps.Create("openai_codex", "verifier1", "state1", "https://example.com/auth", time.Minute)

	if sess.ID == "" {
		t.Fatal("Create: session ID is empty")
	}
	if sess.Provider != "openai_codex" {
		t.Errorf("Provider: got %q, want %q", sess.Provider, "openai_codex")
	}
	if sess.Verifier != "verifier1" {
		t.Errorf("Verifier: got %q, want %q", sess.Verifier, "verifier1")
	}
	if sess.State != "state1" {
		t.Errorf("State: got %q, want %q", sess.State, "state1")
	}
	if sess.AuthorizeURL != "https://example.com/auth" {
		t.Errorf("AuthorizeURL: got %q, want %q", sess.AuthorizeURL, "https://example.com/auth")
	}

	got, ok := ps.Get(sess.ID)
	if !ok {
		t.Fatal("Get: expected found=true, got false")
	}
	if got.ID != sess.ID {
		t.Errorf("Get: ID mismatch: got %q, want %q", got.ID, sess.ID)
	}
}

func TestPendingStore_GetMissing(t *testing.T) {
	ps := NewPendingStore()
	_, ok := ps.Get("nonexistent-id")
	if ok {
		t.Error("Get: expected found=false for nonexistent ID, got true")
	}
}

func TestPendingStore_Delete(t *testing.T) {
	ps := NewPendingStore()
	sess := ps.Create("anthropic", "v", "s", "https://auth.url", time.Minute)

	ps.Delete(sess.ID)

	_, ok := ps.Get(sess.ID)
	if ok {
		t.Error("Get after Delete: expected found=false, got true")
	}
}

func TestPendingStore_ExpiredSessionNotReturned(t *testing.T) {
	ps := NewPendingStore()
	// Create a session that expires immediately (negative TTL puts ExpiresAt in the past).
	sess := ps.Create("anthropic", "v", "s", "https://auth.url", -time.Second)

	_, ok := ps.Get(sess.ID)
	if ok {
		t.Error("Get: expected expired session to not be returned, but got found=true")
	}
}

func TestPendingStore_Cleanup(t *testing.T) {
	ps := NewPendingStore()

	// One session that is already expired.
	expired := ps.Create("anthropic", "v1", "s1", "https://auth.url", -time.Second)
	// One session that is still valid.
	valid := ps.Create("openai_codex", "v2", "s2", "https://auth2.url", time.Minute)

	ps.Cleanup()

	// The expired session should be gone.
	_, ok := ps.Get(expired.ID)
	if ok {
		t.Error("Cleanup: expired session still present after Cleanup")
	}

	// The valid session should still be there.
	_, ok = ps.Get(valid.ID)
	if !ok {
		t.Error("Cleanup: valid session was incorrectly removed by Cleanup")
	}
}

func TestPendingStore_DeleteNoOp(t *testing.T) {
	ps := NewPendingStore()
	// Deleting a nonexistent ID should not panic.
	ps.Delete("does-not-exist")
}
