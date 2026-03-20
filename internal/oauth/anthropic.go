package oauth

// AnthropicProvider returns the OAuthProvider configuration for Anthropic.
func AnthropicProvider() OAuthProvider {
	return OAuthProvider{
		Name:         "anthropic",
		AuthorizeURL: "https://claude.ai/oauth/authorize",
		TokenURL:     "https://platform.claude.com/v1/oauth/token",
		ClientID:     "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		Scopes:       []string{"org:create_api_key", "user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"},
	}
}
