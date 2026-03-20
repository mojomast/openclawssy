package oauth

// OpenAICodexProvider returns the OAuthProvider configuration for OpenAI Codex.
func OpenAICodexProvider() OAuthProvider {
	return OAuthProvider{
		Name:         "openai_codex",
		AuthorizeURL: "https://auth.openai.com/oauth/authorize",
		TokenURL:     "https://auth.openai.com/oauth/token",
		ClientID:     "app_EMoamEEZ73f0CkXaXp7hrann",
		Scopes:       []string{"openid", "profile", "email", "offline_access", "api.connectors.read", "api.connectors.invoke"},
	}
}
