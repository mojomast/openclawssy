package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// tokenEndpointResponse mirrors the JSON body returned by a token endpoint.
type tokenEndpointResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	// Error fields
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ExchangeCode exchanges an OAuth 2.0 authorization code for tokens using
// the PKCE code verifier. For the openai_codex provider it also extracts the
// account ID from the returned access token JWT. The state parameter is
// required by Anthropic's token endpoint.
func ExchangeCode(ctx context.Context, provider OAuthProvider, code, verifier, redirectURI, state string) (TokenResponse, error) {
	if provider.Name == "anthropic" {
		return doAnthropicTokenRequest(ctx, provider, map[string]string{
			"grant_type":    "authorization_code",
			"client_id":     provider.ClientID,
			"code":          code,
			"state":         state,
			"redirect_uri":  redirectURI,
			"code_verifier": verifier,
		})
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {provider.ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	return doTokenRequest(ctx, provider, form)
}

// doAnthropicTokenRequest posts a JSON body to Anthropic's token endpoint.
func doAnthropicTokenRequest(ctx context.Context, provider OAuthProvider, body map[string]string) (TokenResponse, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL,
		strings.NewReader(string(jsonBody)))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: read response: %w", err)
	}

	var raw tokenEndpointResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: decode response (status %d): %w", resp.StatusCode, err)
	}

	if raw.Error != "" {
		desc := raw.ErrorDescription
		if desc == "" {
			desc = raw.Error
		}
		return TokenResponse{}, fmt.Errorf("oauth/exchange: provider error: %s", desc)
	}

	return TokenResponse{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		ExpiresIn:    raw.ExpiresIn,
		TokenType:    raw.TokenType,
	}, nil
}

// RefreshAccessToken uses a refresh token to obtain a new access token.
// For the openai_codex provider it also extracts the account ID from the
// returned access token JWT.
func RefreshAccessToken(ctx context.Context, provider OAuthProvider, refreshToken string) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {provider.ClientID},
		"refresh_token": {refreshToken},
	}
	return doTokenRequest(ctx, provider, form)
}

// doTokenRequest posts a form-encoded body to the provider's token URL and
// parses the JSON response into a TokenResponse.
func doTokenRequest(ctx context.Context, provider OAuthProvider, form url.Values) (TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: read response: %w", err)
	}

	var raw tokenEndpointResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: decode response (status %d): %w", resp.StatusCode, err)
	}

	if raw.Error != "" {
		desc := raw.ErrorDescription
		if desc == "" {
			desc = raw.Error
		}
		return TokenResponse{}, fmt.Errorf("oauth/exchange: provider error: %s", desc)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenResponse{}, fmt.Errorf("oauth/exchange: unexpected status %d", resp.StatusCode)
	}

	tr := TokenResponse{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		ExpiresIn:    raw.ExpiresIn,
		TokenType:    raw.TokenType,
	}

	// For Codex, extract the account ID embedded in the access token JWT.
	if provider.Name == "openai_codex" && raw.AccessToken != "" {
		tr.AccountID = ExtractCodexAccountID(raw.AccessToken)
	}

	return tr, nil
}
