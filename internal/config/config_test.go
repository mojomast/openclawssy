package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	bad := `{"shell":{"enable_exec":true},"sandbox":{"active":false},"server":{"bind_address":"127.0.0.1","port":8080},"workspace":{"root":"./workspace"}}`
	if err := WriteAtomic(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigRoundtripAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Sandbox.Active = true
	cfg.Sandbox.Provider = "local"
	cfg.Shell.EnableExec = true

	if err := Save(path, cfg); err != nil {
		t.Fatalf("first save: %v", err)
	}

	cfg.Server.Port = 9090
	if err := Save(path, cfg); err != nil {
		t.Fatalf("second save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Server.Port != 9090 {
		t.Fatalf("expected server port 9090, got %d", loaded.Server.Port)
	}

	bak := filepath.Join(dir, "config.json.bak")
	if _, err := Load(bak); err != nil {
		t.Fatalf("expected readable backup config, got: %v", err)
	}
}

func TestDefaultConfigSetsMaxTokens(t *testing.T) {
	cfg := Default()
	if cfg.Model.MaxTokens != 20000 {
		t.Fatalf("expected default model.max_tokens=20000, got %d", cfg.Model.MaxTokens)
	}
}

func TestDefaultConfigBindsServerToLoopback(t *testing.T) {
	cfg := Default()
	if cfg.Server.BindAddress != "127.0.0.1" {
		t.Fatalf("expected default server.bind_address=127.0.0.1, got %q", cfg.Server.BindAddress)
	}
}

func TestValidateRejectsOutOfRangeMaxTokens(t *testing.T) {
	cfg := Default()
	cfg.Model.MaxTokens = 25000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for max_tokens > 20000")
	}
}

func TestDefaultConfigSetsThinkingModeNever(t *testing.T) {
	cfg := Default()
	if cfg.Output.ThinkingMode != ThinkingModeNever {
		t.Fatalf("expected default output.thinking_mode=%q, got %q", ThinkingModeNever, cfg.Output.ThinkingMode)
	}
}

func TestApplyDefaultsSetsThinkingModeNever(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Output.ThinkingMode != ThinkingModeNever {
		t.Fatalf("expected thinking_mode default %q, got %q", ThinkingModeNever, cfg.Output.ThinkingMode)
	}
	if cfg.Output.MaxThinkingChars != 4000 {
		t.Fatalf("expected max_thinking_chars default 4000, got %d", cfg.Output.MaxThinkingChars)
	}
}

func TestValidateRejectsInvalidThinkingMode(t *testing.T) {
	cfg := Default()
	cfg.Output.ThinkingMode = "sometimes"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid thinking_mode")
	}
}

func TestValidateRejectsUnsupportedSandboxProvider(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Provider = "kubernetes"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unsupported sandbox provider")
	}
}

func TestValidateAcceptsDockerSandboxProvider(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Provider = "docker"
	// docker is a fully supported sandbox provider
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected docker provider to be accepted by config, got: %v", err)
	}
}

func TestDefaultConfigSetsConcurrencyAndSchedulerDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Engine.MaxConcurrentRuns != 64 {
		t.Fatalf("expected engine.max_concurrent_runs=64, got %d", cfg.Engine.MaxConcurrentRuns)
	}
	if cfg.Engine.DefaultRunTimeoutMS != 20*60*1000 {
		t.Fatalf("expected engine.default_run_timeout_ms=1200000, got %d", cfg.Engine.DefaultRunTimeoutMS)
	}
	if cfg.Engine.MaxRunTimeoutMS != 2*60*60*1000 {
		t.Fatalf("expected engine.max_run_timeout_ms=7200000, got %d", cfg.Engine.MaxRunTimeoutMS)
	}
	if cfg.Scheduler.MaxConcurrentJobs != 4 {
		t.Fatalf("expected scheduler.max_concurrent_jobs=4, got %d", cfg.Scheduler.MaxConcurrentJobs)
	}
	if !cfg.Scheduler.CatchUp {
		t.Fatal("expected scheduler.catch_up=true by default")
	}
}

func TestValidateRejectsInvalidMaxThinkingChars(t *testing.T) {
	cfg := Default()
	cfg.Output.MaxThinkingChars = 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for output.max_thinking_chars")
	}
}

func TestValidateRejectsEmptyShellAllowedCommand(t *testing.T) {
	cfg := Default()
	cfg.Shell.AllowedCommands = []string{"git", "   "}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for empty shell.allowed_commands entry")
	}
}

func TestValidateRejectsInvalidRunTimeoutConfig(t *testing.T) {
	cfg := Default()
	cfg.Engine.DefaultRunTimeoutMS = 500
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for default run timeout below minimum")
	}

	cfg = Default()
	cfg.Engine.MaxRunTimeoutMS = 500
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for max run timeout below minimum")
	}

	cfg = Default()
	cfg.Engine.DefaultRunTimeoutMS = 3000
	cfg.Engine.MaxRunTimeoutMS = 2000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when default run timeout exceeds max run timeout")
	}
}

func TestRedactedClearsSensitiveFieldsOnly(t *testing.T) {
	cfg := Default()
	cfg.Providers.OpenAI.APIKey = "openai-key"
	cfg.Providers.OpenRouter.APIKey = "openrouter-key"
	cfg.Providers.Requesty.APIKey = "requesty-key"
	cfg.Providers.ZAI.APIKey = "zai-key"
	cfg.Providers.Generic.APIKey = "generic-key"
	cfg.Discord.Token = "discord-token"
	cfg.Telegram.Token = "telegram-token"
	cfg.Model.Name = "kept-model"

	redacted := cfg.Redacted()

	if redacted.Providers.OpenAI.APIKey != "" || redacted.Providers.OpenRouter.APIKey != "" || redacted.Providers.Requesty.APIKey != "" || redacted.Providers.ZAI.APIKey != "" || redacted.Providers.Generic.APIKey != "" {
		t.Fatalf("expected provider api keys redacted, got %+v", redacted.Providers)
	}
	if redacted.Discord.Token != "" {
		t.Fatalf("expected discord token redacted, got %q", redacted.Discord.Token)
	}
	if redacted.Telegram.Token != "" {
		t.Fatalf("expected telegram token redacted, got %q", redacted.Telegram.Token)
	}
	if redacted.Model.Name != "kept-model" {
		t.Fatalf("expected non-sensitive model name preserved, got %q", redacted.Model.Name)
	}
}

func TestValidateRejectsTelegramRateLimitUnderOne(t *testing.T) {
	cfg := Default()
	cfg.Telegram.RateLimitPerMin = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for telegram.rate_limit_per_min")
	}
}

func TestValidateRejectsInvalidEnabledAgentID(t *testing.T) {
	cfg := Default()
	cfg.Agents.EnabledAgentIDs = []string{"default", "../evil"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid enabled agent id")
	}
}

func TestValidateRejectsInvalidAgentProfileProvider(t *testing.T) {
	cfg := Default()
	cfg.Agents.Profiles["default"] = AgentProfile{Model: ModelConfig{Provider: "bogus", Name: "x"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid agent profile provider")
	}
}

func TestMemoryDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Memory.Enabled {
		t.Fatal("expected memory.enabled=false by default")
	}
	if cfg.Memory.MaxWorkingItems != 200 {
		t.Fatalf("expected memory.max_working_items=200, got %d", cfg.Memory.MaxWorkingItems)
	}
	if cfg.Memory.MaxPromptTokens != 1200 {
		t.Fatalf("expected memory.max_prompt_tokens=1200, got %d", cfg.Memory.MaxPromptTokens)
	}
	if cfg.Memory.AutoCheckpoint {
		t.Fatal("expected memory.auto_checkpoint=false by default")
	}
	if !cfg.Memory.ProactiveEnabled {
		t.Fatal("expected memory.proactive_enabled=true by default")
	}
	if cfg.Memory.EventBufferSize != 256 {
		t.Fatalf("expected memory.event_buffer_size=256, got %d", cfg.Memory.EventBufferSize)
	}
	if cfg.Memory.EmbeddingsEnabled {
		t.Fatal("expected memory.embeddings_enabled=false by default")
	}
	if cfg.Memory.EmbeddingProvider != "openrouter" {
		t.Fatalf("expected memory.embedding_provider=openrouter, got %q", cfg.Memory.EmbeddingProvider)
	}
	if cfg.Memory.EmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("expected memory.embedding_model=text-embedding-3-small, got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestApplyDefaultsSetsMemoryDefaults(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Memory.MaxWorkingItems != 200 {
		t.Fatalf("expected memory.max_working_items default 200, got %d", cfg.Memory.MaxWorkingItems)
	}
	if cfg.Memory.MaxPromptTokens != 1200 {
		t.Fatalf("expected memory.max_prompt_tokens default 1200, got %d", cfg.Memory.MaxPromptTokens)
	}
	if cfg.Memory.EventBufferSize != 256 {
		t.Fatalf("expected memory.event_buffer_size default 256, got %d", cfg.Memory.EventBufferSize)
	}
	if cfg.Memory.EmbeddingProvider != "openrouter" {
		t.Fatalf("expected memory.embedding_provider default openrouter, got %q", cfg.Memory.EmbeddingProvider)
	}
	if cfg.Memory.EmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("expected memory.embedding_model default text-embedding-3-small, got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestValidateRejectsInvalidMemoryConfig(t *testing.T) {
	cfg := Default()
	cfg.Memory.MaxWorkingItems = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for memory.max_working_items")
	}

	cfg = Default()
	cfg.Memory.MaxPromptTokens = 10
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for memory.max_prompt_tokens")
	}

	cfg = Default()
	cfg.Memory.EventBufferSize = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for memory.event_buffer_size")
	}

	cfg = Default()
	cfg.Memory.EmbeddingProvider = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for memory.embedding_provider")
	}

	cfg = Default()
	cfg.Memory.EmbeddingModel = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for memory.embedding_model")
	}
}

func TestApplyDefaultsSetsHardenedDockerPidsLimit(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Docker.Hardened = true
	cfg.Sandbox.Docker.PidsLimit = 0
	cfg.ApplyDefaults()
	if cfg.Sandbox.Docker.PidsLimit != 256 {
		t.Fatalf("expected hardened pids_limit default 256, got %d", cfg.Sandbox.Docker.PidsLimit)
	}
}

func TestValidateRejectsDedicatedDaemonWithoutHost(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Docker.RequireDedicatedDaemon = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when dedicated daemon is required without sandbox.docker.host")
	}
}

func TestValidateRejectsDefaultSocketWhenDedicatedRequired(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Docker.RequireDedicatedDaemon = true
	cfg.Sandbox.Docker.Host = "unix:///var/run/docker.sock"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when dedicated daemon uses default host socket")
	}
}

func TestValidateAcceptsDedicatedDaemonHost(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Docker.RequireDedicatedDaemon = true
	cfg.Sandbox.Docker.Host = "unix:///var/run/openclawssy-docker.sock"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected dedicated daemon config to validate, got %v", err)
	}
}

func TestOpenClawRemoteDefaults(t *testing.T) {
	cfg := Default()
	if strings.TrimSpace(cfg.OpenClaw.Remote.RepositoryURL) == "" {
		t.Fatal("expected openclaw remote repository_url default to be set")
	}
	if strings.TrimSpace(cfg.OpenClaw.Remote.BinaryPath) == "" {
		t.Fatal("expected openclaw remote binary_path default to be set")
	}
	if cfg.OpenClaw.Remote.WSPrimary != "wss://kimi.tailec998.ts.net" {
		t.Fatalf("unexpected openclaw remote ws_primary default: %q", cfg.OpenClaw.Remote.WSPrimary)
	}
	if cfg.OpenClaw.Remote.SessionKey != "agent:main:main" {
		t.Fatalf("unexpected openclaw remote session key default: %q", cfg.OpenClaw.Remote.SessionKey)
	}
	if !cfg.OpenClaw.Remote.PreferTailnetWSS {
		t.Fatal("expected openclaw remote prefer_tailnet_wss default true")
	}
}

func TestValidateRejectsInvalidOpenClawRemoteURL(t *testing.T) {
	cfg := Default()
	cfg.OpenClaw.Remote.WSPrimary = "https://example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for non-websocket ws_primary")
	}
}

func TestSubAgentRestrictionsValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr string
	}{
		{
			"valid defaults",
			func(c *Config) {},
			"",
		},
		{
			"invalid thinking_mode in defaults",
			func(c *Config) { c.Agents.SubAgentDefaults.ThinkingMode = "off" },
			"thinking_mode",
		},
		{
			"invalid delegation_mode in defaults",
			func(c *Config) { c.Agents.SubAgentDefaults.DelegationMode = "bogus" },
			"delegation_mode",
		},
		{
			"negative max_tool_iterations",
			func(c *Config) { c.Agents.SubAgentDefaults.MaxToolIterations = -1 },
			"max_tool_iterations",
		},
		{
			"negative timeout_ms",
			func(c *Config) { c.Agents.SubAgentDefaults.TimeoutMS = -1 },
			"timeout_ms",
		},
		{
			"valid override",
			func(c *Config) {
				c.Agents.SubAgentOverrides["research"] = SubAgentRestrictions{
					AllowedTools: []string{"fs.read", "code.search"},
					ThinkingMode: "on_error",
				}
			},
			"",
		},
		{
			"invalid thinking_mode in override",
			func(c *Config) {
				c.Agents.SubAgentOverrides["bad"] = SubAgentRestrictions{
					ThinkingMode: "off",
				}
			},
			"thinking_mode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestSubAgentDefaultsApplied(t *testing.T) {
	cfg := Config{} // empty config
	cfg.ApplyDefaults()
	if len(cfg.Agents.SubAgentDefaults.AllowedTools) == 0 {
		t.Fatal("expected subagent defaults to be populated after ApplyDefaults")
	}
	if cfg.Agents.SubAgentDefaults.ThinkingMode != "never" {
		t.Fatalf("expected default thinking_mode=never, got %q", cfg.Agents.SubAgentDefaults.ThinkingMode)
	}
	if cfg.Agents.SubAgentOverrides == nil {
		t.Fatal("expected SubAgentOverrides to be initialized after ApplyDefaults")
	}
}

func TestApplyDefaultsPreservesExplicitSubAgentFields(t *testing.T) {
	cfg := Config{}
	cfg.Agents.SubAgentDefaults.TimeoutMS = 45000
	cfg.Agents.SubAgentDefaults.ThinkingMode = ThinkingModeAlways

	cfg.ApplyDefaults()

	if cfg.Agents.SubAgentDefaults.TimeoutMS != 45000 {
		t.Fatalf("expected explicit timeout preserved, got %d", cfg.Agents.SubAgentDefaults.TimeoutMS)
	}
	if cfg.Agents.SubAgentDefaults.ThinkingMode != ThinkingModeAlways {
		t.Fatalf("expected explicit thinking mode preserved, got %q", cfg.Agents.SubAgentDefaults.ThinkingMode)
	}
	if len(cfg.Agents.SubAgentDefaults.AllowedTools) == 0 {
		t.Fatal("expected default allowed tools to be filled when omitted")
	}
}
