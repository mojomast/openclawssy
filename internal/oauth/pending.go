package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// PendingSession represents an in-progress OAuth login initiated by the
// dashboard. It carries the PKCE verifier and other state needed to
// complete the exchange once the callback is received.
type PendingSession struct {
	ID           string
	Provider     string
	Verifier     string
	State        string
	AuthorizeURL string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// PendingStore holds active pending sessions with TTL-based expiry.
type PendingStore struct {
	mu       sync.Mutex
	sessions map[string]PendingSession
}

// NewPendingStore creates an empty PendingStore.
func NewPendingStore() *PendingStore {
	return &PendingStore{
		sessions: make(map[string]PendingSession),
	}
}

// Create adds a new pending session and returns it. The session ID is a
// random hex string. The caller supplies the PKCE verifier, OAuth state
// nonce, the full authorize URL, and the desired TTL.
func (ps *PendingStore) Create(provider, verifier, state, authorizeURL string, ttl time.Duration) PendingSession {
	id := generateSessionID()
	now := time.Now().UTC()
	sess := PendingSession{
		ID:           id,
		Provider:     provider,
		Verifier:     verifier,
		State:        state,
		AuthorizeURL: authorizeURL,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
	}
	ps.mu.Lock()
	ps.sessions[id] = sess
	ps.mu.Unlock()
	return sess
}

// Get returns the session with the given ID if it exists and has not expired.
func (ps *PendingStore) Get(id string) (PendingSession, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	sess, ok := ps.sessions[id]
	if !ok {
		return PendingSession{}, false
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		delete(ps.sessions, id)
		return PendingSession{}, false
	}
	return sess, true
}

// Delete removes the session with the given ID. No-op if not present.
func (ps *PendingStore) Delete(id string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.sessions, id)
}

// Cleanup removes all sessions whose ExpiresAt is in the past.
func (ps *PendingStore) Cleanup() {
	now := time.Now().UTC()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for id, sess := range ps.sessions {
		if now.After(sess.ExpiresAt) {
			delete(ps.sessions, id)
		}
	}
}

// generateSessionID returns a 16-byte random hex string.
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; panic would be inappropriate in production code,
		// but rand.Read only fails on OS-level failures where the process
		// cannot safely continue anyway.
		panic("oauth/pending: failed to generate session ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
