package oauth

import (
	"sync"
	"testing"
	"time"
)

// memStore is a simple in-memory SecretStore for tests.
type memStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemStore() *memStore {
	return &memStore{data: make(map[string]string)}
}

func (m *memStore) Get(name string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[name]
	return v, ok, nil
}

func (m *memStore) Set(name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[name] = value
	return nil
}

func (m *memStore) Delete(name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[name]
	delete(m.data, name)
	return ok, nil
}

func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.data))
	for k := range m.data {
		out = append(out, k)
	}
	return out
}

func TestStoreRoundTrip(t *testing.T) {
	store := newMemStore()
	now := time.Now().UTC().Truncate(time.Second) // RFC3339 has second precision

	creds := Credentials{
		AccessToken:  "at_test",
		RefreshToken: "rt_test",
		ExpiresAt:    now,
		AccountID:    "acct_test",
	}

	if err := SaveCredentials(store, "myprovider", creds); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	got, ok, err := LoadCredentials(store, "myprovider")
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if !ok {
		t.Fatal("LoadCredentials: expected found=true, got false")
	}

	if got.AccessToken != creds.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, creds.AccessToken)
	}
	if got.RefreshToken != creds.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, creds.RefreshToken)
	}
	if !got.ExpiresAt.Equal(creds.ExpiresAt) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, creds.ExpiresAt)
	}
	if got.AccountID != creds.AccountID {
		t.Errorf("AccountID: got %q, want %q", got.AccountID, creds.AccountID)
	}
}

func TestLoadCredentials_Missing(t *testing.T) {
	store := newMemStore()
	_, ok, err := LoadCredentials(store, "nonexistent")
	if err != nil {
		t.Fatalf("LoadCredentials: unexpected error: %v", err)
	}
	if ok {
		t.Error("expected found=false for missing provider, got true")
	}
}

func TestDeleteCredentials_ClearsAllKeys(t *testing.T) {
	store := newMemStore()
	creds := Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().UTC(),
		AccountID:    "aid",
	}
	if err := SaveCredentials(store, "myprovider", creds); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	if err := DeleteCredentials(store, "myprovider"); err != nil {
		t.Fatalf("DeleteCredentials: %v", err)
	}

	_, ok, err := LoadCredentials(store, "myprovider")
	if err != nil {
		t.Fatalf("LoadCredentials after delete: %v", err)
	}
	if ok {
		t.Error("expected found=false after delete, got true")
	}

	if remaining := store.keys(); len(remaining) != 0 {
		t.Errorf("expected empty store after delete, found keys: %v", remaining)
	}
}

func TestSaveCredentials_EmptyRefreshToken(t *testing.T) {
	store := newMemStore()
	creds := Credentials{
		AccessToken:  "at_only",
		RefreshToken: "", // intentionally empty
		ExpiresAt:    time.Time{},
		AccountID:    "",
	}
	if err := SaveCredentials(store, "provider2", creds); err != nil {
		t.Fatalf("SaveCredentials with empty refresh_token: %v", err)
	}

	got, ok, err := LoadCredentials(store, "provider2")
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true")
	}
	if got.AccessToken != "at_only" {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "at_only")
	}
	if got.RefreshToken != "" {
		t.Errorf("RefreshToken: got %q, want empty", got.RefreshToken)
	}
}

func TestStoreRoundTrip_ExpiresAt(t *testing.T) {
	store := newMemStore()
	// Use a specific timestamp to verify round-trip precision.
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	creds := Credentials{
		AccessToken: "at",
		ExpiresAt:   ts,
	}
	if err := SaveCredentials(store, "p", creds); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	got, _, err := LoadCredentials(store, "p")
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if !got.ExpiresAt.Equal(ts) {
		t.Errorf("ExpiresAt round-trip: got %v, want %v", got.ExpiresAt, ts)
	}
}
