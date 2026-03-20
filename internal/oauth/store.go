package oauth

import (
	"fmt"
	"time"
)

// SecretStore is the subset of secrets.Store that credential storage needs.
type SecretStore interface {
	Get(name string) (string, bool, error)
	Set(name, value string) error
	Delete(name string) (bool, error)
}

// Credentials holds the OAuth tokens and metadata for a single provider.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountID    string // codex only; empty for other providers
}

func keyAccessToken(provider string) string  { return fmt.Sprintf("oauth/%s/access_token", provider) }
func keyRefreshToken(provider string) string { return fmt.Sprintf("oauth/%s/refresh_token", provider) }
func keyExpiresAt(provider string) string    { return fmt.Sprintf("oauth/%s/expires_at", provider) }
func keyAccountID(provider string) string    { return fmt.Sprintf("oauth/%s/account_id", provider) }

// SaveCredentials persists all credential fields for the given provider into
// the secret store. Empty string values are still stored so that a
// subsequent load can reconstruct the struct faithfully.
func SaveCredentials(store SecretStore, provider string, creds Credentials) error {
	if err := store.Set(keyAccessToken(provider), creds.AccessToken); err != nil {
		return fmt.Errorf("oauth/store: save access_token: %w", err)
	}
	if err := store.Set(keyRefreshToken(provider), creds.RefreshToken); err != nil {
		return fmt.Errorf("oauth/store: save refresh_token: %w", err)
	}
	var expiresAt string
	if !creds.ExpiresAt.IsZero() {
		expiresAt = creds.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if err := store.Set(keyExpiresAt(provider), expiresAt); err != nil {
		return fmt.Errorf("oauth/store: save expires_at: %w", err)
	}
	if err := store.Set(keyAccountID(provider), creds.AccountID); err != nil {
		return fmt.Errorf("oauth/store: save account_id: %w", err)
	}
	return nil
}

// LoadCredentials reads the stored credentials for the given provider.
// Returns false (and no error) if the access token is not present.
func LoadCredentials(store SecretStore, provider string) (Credentials, bool, error) {
	accessToken, ok, err := store.Get(keyAccessToken(provider))
	if err != nil {
		return Credentials{}, false, fmt.Errorf("oauth/store: load access_token: %w", err)
	}
	if !ok {
		return Credentials{}, false, nil
	}

	refreshToken, _, err := store.Get(keyRefreshToken(provider))
	if err != nil {
		return Credentials{}, false, fmt.Errorf("oauth/store: load refresh_token: %w", err)
	}

	expiresAtStr, _, err := store.Get(keyExpiresAt(provider))
	if err != nil {
		return Credentials{}, false, fmt.Errorf("oauth/store: load expires_at: %w", err)
	}
	var expiresAt time.Time
	if expiresAtStr != "" {
		expiresAt, err = time.Parse(time.RFC3339, expiresAtStr)
		if err != nil {
			return Credentials{}, false, fmt.Errorf("oauth/store: parse expires_at %q: %w", expiresAtStr, err)
		}
	}

	accountID, _, err := store.Get(keyAccountID(provider))
	if err != nil {
		return Credentials{}, false, fmt.Errorf("oauth/store: load account_id: %w", err)
	}

	return Credentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		AccountID:    accountID,
	}, true, nil
}

// DeleteCredentials removes all credential keys for the given provider from
// the store. It is not an error if any key was already absent.
func DeleteCredentials(store SecretStore, provider string) error {
	for _, key := range []string{
		keyAccessToken(provider),
		keyRefreshToken(provider),
		keyExpiresAt(provider),
		keyAccountID(provider),
	} {
		if _, err := store.Delete(key); err != nil {
			return fmt.Errorf("oauth/store: delete %s: %w", key, err)
		}
	}
	return nil
}
