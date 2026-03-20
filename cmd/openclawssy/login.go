package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/oauth"
	"openclawssy/internal/secrets"
)

func handleLogin(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: openclawssy login <provider>")
		fmt.Fprintln(os.Stderr, "       openclawssy login --list")
		fmt.Fprintln(os.Stderr, "       openclawssy login --logout <provider>")
		fmt.Fprintln(os.Stderr, "providers: openai_codex, anthropic")
		return 2
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))

	switch command {
	case "--list":
		return handleLoginList()
	case "--logout":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: openclawssy login --logout <provider>")
			return 2
		}
		return handleLoginLogout(args[1])
	default:
		return handleLoginProvider(ctx, command)
	}
}

func handleLoginList() int {
	cfg, err := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 1
	}
	store, err := secrets.NewStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "secret store error:", err)
		return 1
	}

	providers := []string{"openai_codex", "anthropic"}
	fmt.Println("OAuth provider status:")
	for _, p := range providers {
		creds, found, err := oauth.LoadCredentials(store, p)
		if err != nil {
			fmt.Printf("  %-15s error: %v\n", p, err)
			continue
		}
		if !found {
			fmt.Printf("  %-15s not logged in\n", p)
			continue
		}
		status := "logged in"
		if !creds.ExpiresAt.IsZero() {
			if time.Now().After(creds.ExpiresAt) {
				status = "expired (refresh needed)"
			} else {
				status = fmt.Sprintf("logged in (expires %s)", creds.ExpiresAt.Format(time.RFC3339))
			}
		}
		if creds.AccountID != "" {
			status += fmt.Sprintf(" account=%s", creds.AccountID)
		}
		fmt.Printf("  %-15s %s\n", p, status)
	}
	return 0
}

func handleLoginLogout(providerName string) int {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	cfg, err := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 1
	}
	store, err := secrets.NewStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "secret store error:", err)
		return 1
	}
	if err := oauth.DeleteCredentials(store, providerName); err != nil {
		fmt.Fprintln(os.Stderr, "logout error:", err)
		return 1
	}
	fmt.Printf("logged out from %s\n", providerName)
	return 0
}

func handleLoginProvider(ctx context.Context, providerName string) int {
	provider, ok := oauth.ProviderByName(providerName)
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported oauth provider: %s\nSupported: openai_codex, anthropic\n", providerName)
		return 2
	}

	cfg, err := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 1
	}
	if err := ensureMasterKey(cfg.Secrets.MasterKeyFile); err != nil {
		fmt.Fprintln(os.Stderr, "master key error:", err)
		return 1
	}
	store, err := secrets.NewStore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "secret store error:", err)
		return 1
	}

	// Generate PKCE
	verifier, err := oauth.GenerateVerifier()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pkce error:", err)
		return 1
	}
	challenge := oauth.ChallengeFromVerifier(verifier)

	// Try to start local callback server
	callbackCtx, callbackCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer callbackCancel()

	// Provider-specific callback ports (must match registered redirect URIs).
	listenPort := 0
	callbackPath := "/auth/callback"
	switch provider.Name {
	case "openai_codex":
		listenPort = 1455
	case "anthropic":
		listenPort = 53692
		callbackPath = "/callback"
	}

	port, resultCh, shutdown, err := oauth.StartCallbackServer(callbackCtx, listenPort)
	if err != nil && listenPort != 0 {
		fmt.Fprintf(os.Stderr, "Port %d is required for %s OAuth but is in use.\n", listenPort, provider.Name)
		fmt.Fprintln(os.Stderr, "Stop the process using that port first, then retry.")
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "callback server error:", err)
		return 1
	}
	defer shutdown()

	redirectURI := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)

	// Anthropic uses the PKCE verifier as the OAuth state parameter.
	// OpenAI Codex uses a random state.
	state := verifier
	if provider.Name == "openai_codex" {
		stateBytes := make([]byte, 32)
		if _, err := rand.Read(stateBytes); err != nil {
			fmt.Fprintln(os.Stderr, "state generation error:", err)
			return 1
		}
		state = base64.RawURLEncoding.EncodeToString(stateBytes)
	}

	// Build authorize URL with provider-appropriate parameters
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", provider.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("scope", strings.Join(provider.Scopes, " "))
	params.Set("state", state)
	if provider.Name == "openai_codex" {
		params.Set("codex_cli_simplified_flow", "true")
		params.Set("originator", "openclawssy")
	}
	if provider.Name == "anthropic" {
		params.Set("code", "true")
	}
	authorizeURL := provider.AuthorizeURL + "?" + params.Encode()

	fmt.Printf("Opening browser for %s login...\n", provider.Name)
	fmt.Printf("If the browser doesn't open, visit this URL:\n\n  %s\n\n", authorizeURL)

	_ = oauth.OpenBrowser(authorizeURL)

	fmt.Println("Waiting for callback (or paste the callback URL/code here)...")

	// Wait for callback or manual paste
	var code string
	select {
	case result := <-resultCh:
		if result.Error != "" {
			fmt.Fprintf(os.Stderr, "oauth error: %s\n", result.Error)
			return 1
		}
		code = result.Code
	case <-callbackCtx.Done():
		fmt.Fprintln(os.Stderr, "login timed out")
		return 1
	}

	if code == "" {
		fmt.Fprintln(os.Stderr, "no authorization code received")
		return 1
	}

	fmt.Println("Exchanging authorization code...")

	tokenResp, err := oauth.ExchangeCode(ctx, provider, code, verifier, redirectURI, state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "token exchange failed: %v\n", err)
		return 1
	}

	creds := oauth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountID:    tokenResp.AccountID,
	}
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	if err := oauth.SaveCredentials(store, provider.Name, creds); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save credentials: %v\n", err)
		return 1
	}

	fmt.Printf("\nSuccessfully logged in to %s\n", provider.Name)
	if creds.AccountID != "" {
		fmt.Printf("Account ID: %s\n", creds.AccountID)
	}
	if !creds.ExpiresAt.IsZero() {
		fmt.Printf("Token expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))
	}
	return 0
}
