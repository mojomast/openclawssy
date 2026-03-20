package oauth

// OAuthProvider holds the static configuration for an OAuth 2.0 provider.
type OAuthProvider struct {
	Name         string
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	Scopes       []string
}

// TokenResponse holds the tokens returned by a provider's token endpoint.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds
	TokenType    string
	AccountID    string // extracted for codex
}

// registry holds all built-in providers, keyed by name.
var registry = map[string]func() OAuthProvider{
	"openai_codex": OpenAICodexProvider,
	"anthropic":    AnthropicProvider,
}

// ProviderByName returns the named provider and true, or the zero value and
// false if no provider with that name is registered.
func ProviderByName(name string) (OAuthProvider, bool) {
	fn, ok := registry[name]
	if !ok {
		return OAuthProvider{}, false
	}
	return fn(), true
}
