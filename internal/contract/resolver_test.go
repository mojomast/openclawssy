package contract

import (
	"reflect"
	"strings"
	"testing"

	"openclawssy/internal/config"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentID   string
		opts      *ResolveOptions
		configure func(*config.Config)
		assert    func(t *testing.T, cfg config.Config, got AgentContract)
		wantErr   string
	}{
		{
			name:    "resolves global defaults for base agent",
			agentID: "default",
			assert: func(t *testing.T, cfg config.Config, got AgentContract) {
				t.Helper()

				if got.Identity.AgentID != "default" {
					t.Fatalf("Identity.AgentID = %q, want %q", got.Identity.AgentID, "default")
				}
				if got.ModelPolicy.Provider != cfg.Model.Provider {
					t.Fatalf("ModelPolicy.Provider = %q, want %q", got.ModelPolicy.Provider, cfg.Model.Provider)
				}
				if got.ModelPolicy.Model != cfg.Model.Name {
					t.Fatalf("ModelPolicy.Model = %q, want %q", got.ModelPolicy.Model, cfg.Model.Name)
				}
				if got.ModelPolicy.TimeoutMS != cfg.Model.TimeoutMS {
					t.Fatalf("ModelPolicy.TimeoutMS = %d, want %d", got.ModelPolicy.TimeoutMS, cfg.Model.TimeoutMS)
				}
				if !reflect.DeepEqual(got.ToolPolicy.AllowedTools, cfg.Agents.SubAgentDefaults.AllowedTools) {
					t.Fatalf("ToolPolicy.AllowedTools = %v, want %v", got.ToolPolicy.AllowedTools, cfg.Agents.SubAgentDefaults.AllowedTools)
				}
				if got.DelegationPolicy.Mode != cfg.Agents.DelegationMode {
					t.Fatalf("DelegationPolicy.Mode = %q, want %q", got.DelegationPolicy.Mode, cfg.Agents.DelegationMode)
				}
				if got.DelegationPolicy.MaxDepth != cfg.Agents.SubAgentDefaults.MaxToolIterations {
					t.Fatalf("DelegationPolicy.MaxDepth = %d, want %d", got.DelegationPolicy.MaxDepth, cfg.Agents.SubAgentDefaults.MaxToolIterations)
				}
				if !strings.Contains(got.SystemPrompt.Content, "## SOUL.md") {
					t.Fatalf("SystemPrompt.Content missing SOUL.md block: %q", got.SystemPrompt.Content)
				}
				assertInheritanceSource(t, got, "model_policy", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "model_policy.provider", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "tool_policy.allowed_tools", InheritanceSourceGlobal)
			},
		},
		{
			name:    "applies agent profile non zero model overrides",
			agentID: "coder",
			configure: func(cfg *config.Config) {
				cfg.Agents.Profiles["coder"] = config.AgentProfile{
					Model: config.ModelConfig{
						Provider:    "openrouter",
						Name:        "moonshot/test",
						Temperature: 0.73,
						MaxTokens:   1024,
						TimeoutMS:   45000,
					},
				}
			},
			assert: func(t *testing.T, _ config.Config, got AgentContract) {
				t.Helper()

				if got.ModelPolicy.Provider != "openrouter" {
					t.Fatalf("ModelPolicy.Provider = %q, want %q", got.ModelPolicy.Provider, "openrouter")
				}
				if got.ModelPolicy.Model != "moonshot/test" {
					t.Fatalf("ModelPolicy.Model = %q, want %q", got.ModelPolicy.Model, "moonshot/test")
				}
				if got.ModelPolicy.Temperature != 0.73 {
					t.Fatalf("ModelPolicy.Temperature = %v, want %v", got.ModelPolicy.Temperature, 0.73)
				}
				if got.ModelPolicy.MaxTokens != 1024 {
					t.Fatalf("ModelPolicy.MaxTokens = %d, want %d", got.ModelPolicy.MaxTokens, 1024)
				}
				if got.ModelPolicy.TimeoutMS != 45000 {
					t.Fatalf("ModelPolicy.TimeoutMS = %d, want %d", got.ModelPolicy.TimeoutMS, 45000)
				}
				assertInheritanceSource(t, got, "model_policy", InheritanceSourceAgentProfile)
				assertInheritanceSource(t, got, "model_policy.provider", InheritanceSourceAgentProfile)
				assertInheritanceSource(t, got, "model_policy.model", InheritanceSourceAgentProfile)
				assertInheritanceSource(t, got, "model_policy.temperature", InheritanceSourceAgentProfile)
				assertInheritanceSource(t, got, "model_policy.max_tokens", InheritanceSourceAgentProfile)
				assertInheritanceSource(t, got, "model_policy.timeout_ms", InheritanceSourceAgentProfile)
			},
		},
		{
			name:    "zero value profile fields do not override parent values",
			agentID: "zero-profile",
			configure: func(cfg *config.Config) {
				cfg.Agents.Profiles["zero-profile"] = config.AgentProfile{Model: config.ModelConfig{}}
			},
			assert: func(t *testing.T, cfg config.Config, got AgentContract) {
				t.Helper()

				if got.ModelPolicy.Provider != cfg.Model.Provider {
					t.Fatalf("ModelPolicy.Provider = %q, want %q", got.ModelPolicy.Provider, cfg.Model.Provider)
				}
				if got.ModelPolicy.TimeoutMS != cfg.Model.TimeoutMS {
					t.Fatalf("ModelPolicy.TimeoutMS = %d, want %d", got.ModelPolicy.TimeoutMS, cfg.Model.TimeoutMS)
				}
				assertInheritanceSource(t, got, "model_policy", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "model_policy.timeout_ms", InheritanceSourceGlobal)
			},
		},
		{
			name:    "applies subagent defaults in subagent context",
			agentID: "default",
			opts:    &ResolveOptions{SubAgent: true},
			configure: func(cfg *config.Config) {
				cfg.Agents.SubAgentDefaults = config.SubAgentRestrictions{
					AllowedTools:      []string{"fs.read", "code.search"},
					MaxToolIterations: 42,
					TimeoutMS:         333000,
					ThinkingMode:      config.ThinkingModeAlways,
					DelegationMode:    "tool_gated",
				}
			},
			assert: func(t *testing.T, _ config.Config, got AgentContract) {
				t.Helper()

				if !reflect.DeepEqual(got.ToolPolicy.AllowedTools, []string{"fs.read", "code.search"}) {
					t.Fatalf("ToolPolicy.AllowedTools = %v, want [fs.read code.search]", got.ToolPolicy.AllowedTools)
				}
				if got.DelegationPolicy.Mode != "tool_gated" {
					t.Fatalf("DelegationPolicy.Mode = %q, want %q", got.DelegationPolicy.Mode, "tool_gated")
				}
				if got.DelegationPolicy.MaxDepth != 42 {
					t.Fatalf("DelegationPolicy.MaxDepth = %d, want %d", got.DelegationPolicy.MaxDepth, 42)
				}
				if got.ModelPolicy.TimeoutMS != 333000 {
					t.Fatalf("ModelPolicy.TimeoutMS = %d, want %d", got.ModelPolicy.TimeoutMS, 333000)
				}
				if got.ObservabilityPolicy.ThinkingMode != config.ThinkingModeAlways {
					t.Fatalf("ObservabilityPolicy.ThinkingMode = %q, want %q", got.ObservabilityPolicy.ThinkingMode, config.ThinkingModeAlways)
				}
				assertInheritanceSource(t, got, "tool_policy.allowed_tools", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "delegation_policy.mode", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "delegation_policy.max_depth", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "model_policy.timeout_ms", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "observability_policy.thinking_mode", InheritanceSourceGlobal)
			},
		},
		{
			name:    "subagent override merges and zero value fields fall back to defaults",
			agentID: "research",
			opts:    &ResolveOptions{SubAgent: true},
			configure: func(cfg *config.Config) {
				cfg.Agents.SubAgentDefaults = config.SubAgentRestrictions{
					AllowedTools:      []string{"fs.read"},
					MaxToolIterations: 9,
					TimeoutMS:         120000,
					ThinkingMode:      config.ThinkingModeNever,
					DelegationMode:    "prompt_only",
				}
				cfg.Agents.SubAgentOverrides["research"] = config.SubAgentRestrictions{
					AllowedTools:      []string{"fs.read", "http.request"},
					MaxToolIterations: 0,
					TimeoutMS:         5000,
					ThinkingMode:      "",
					DelegationMode:    "auto_execute",
				}
			},
			assert: func(t *testing.T, _ config.Config, got AgentContract) {
				t.Helper()

				if !reflect.DeepEqual(got.ToolPolicy.AllowedTools, []string{"fs.read", "http.request"}) {
					t.Fatalf("ToolPolicy.AllowedTools = %v, want [fs.read http.request]", got.ToolPolicy.AllowedTools)
				}
				if got.DelegationPolicy.MaxDepth != 9 {
					t.Fatalf("DelegationPolicy.MaxDepth = %d, want %d", got.DelegationPolicy.MaxDepth, 9)
				}
				if got.ModelPolicy.TimeoutMS != 5000 {
					t.Fatalf("ModelPolicy.TimeoutMS = %d, want %d", got.ModelPolicy.TimeoutMS, 5000)
				}
				if got.ObservabilityPolicy.ThinkingMode != config.ThinkingModeNever {
					t.Fatalf("ObservabilityPolicy.ThinkingMode = %q, want %q", got.ObservabilityPolicy.ThinkingMode, config.ThinkingModeNever)
				}
				if got.DelegationPolicy.Mode != "auto_execute" {
					t.Fatalf("DelegationPolicy.Mode = %q, want %q", got.DelegationPolicy.Mode, "auto_execute")
				}

				assertInheritanceSource(t, got, "tool_policy", InheritanceSourceSubagentOverride)
				assertInheritanceSource(t, got, "tool_policy.allowed_tools", InheritanceSourceSubagentOverride)
				assertInheritanceSource(t, got, "delegation_policy.mode", InheritanceSourceSubagentOverride)
				assertInheritanceSource(t, got, "delegation_policy.max_depth", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "model_policy.timeout_ms", InheritanceSourceSubagentOverride)
				assertInheritanceSource(t, got, "observability_policy.thinking_mode", InheritanceSourceGlobal)
			},
		},
		{
			name:    "subagent defaults override parent profile timeout in subagent context",
			agentID: "builder",
			opts:    &ResolveOptions{SubAgent: true},
			configure: func(cfg *config.Config) {
				cfg.Agents.Profiles["builder"] = config.AgentProfile{
					Model: config.ModelConfig{TimeoutMS: 25000},
				}
				cfg.Agents.SubAgentDefaults.TimeoutMS = 60000
			},
			assert: func(t *testing.T, _ config.Config, got AgentContract) {
				t.Helper()

				if got.ModelPolicy.TimeoutMS != 60000 {
					t.Fatalf("ModelPolicy.TimeoutMS = %d, want %d", got.ModelPolicy.TimeoutMS, 60000)
				}
				assertInheritanceSource(t, got, "model_policy.timeout_ms", InheritanceSourceGlobal)
			},
		},
		{
			name:    "unknown agent id resolves using global defaults",
			agentID: "unknown-agent",
			assert: func(t *testing.T, _ config.Config, got AgentContract) {
				t.Helper()

				if got.Identity.AgentID != "unknown-agent" {
					t.Fatalf("Identity.AgentID = %q, want %q", got.Identity.AgentID, "unknown-agent")
				}
				assertInheritanceSource(t, got, "identity", InheritanceSourceGlobal)
				assertInheritanceSource(t, got, "model_policy", InheritanceSourceGlobal)
			},
		},
		{
			name:    "empty agent id returns error",
			agentID: "   ",
			wantErr: "agent id",
		},
		{
			name:    "uses clawdefuckifier prompt scaffold when agent id matches",
			agentID: "clawdefuckifier-worker",
			assert: func(t *testing.T, _ config.Config, got AgentContract) {
				t.Helper()

				if !strings.Contains(got.SystemPrompt.Content, "You are ClawDefuckifier") {
					t.Fatalf("SystemPrompt.Content does not include clawdefuckifier scaffold content")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Default()
			if tt.configure != nil {
				tt.configure(&cfg)
			}

			got, err := Resolve(cfg, tt.agentID, tt.opts)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Resolve() error = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
					t.Fatalf("Resolve() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("resolved contract Validate() error = %v", err)
			}

			for _, key := range []string{
				"identity.agent_id",
				"system_prompt.content",
				"tool_policy.allowed_tools",
				"delegation_policy.mode",
				"memory_policy.enabled",
				"model_policy.timeout_ms",
				"sandbox_policy.provider",
				"observability_policy.thinking_mode",
			} {
				if _, ok := got.Inheritance.Source[key]; !ok {
					t.Fatalf("inheritance.source missing key %q", key)
				}
			}

			if tt.assert != nil {
				tt.assert(t, cfg, got)
			}
		})
	}
}

func TestResolverMethodResolve(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	resolver := NewResolver(cfg)
	got, err := resolver.Resolve("default", nil)
	if err != nil {
		t.Fatalf("resolver.Resolve() error = %v", err)
	}
	if got.Identity.AgentID != "default" {
		t.Fatalf("Identity.AgentID = %q, want default", got.Identity.AgentID)
	}
}

func assertInheritanceSource(t *testing.T, c AgentContract, key, want string) {
	t.Helper()
	got, ok := c.Inheritance.Source[key]
	if !ok {
		t.Fatalf("inheritance.source missing key %q", key)
	}
	if got != want {
		t.Fatalf("inheritance.source[%q] = %q, want %q", key, got, want)
	}
}
