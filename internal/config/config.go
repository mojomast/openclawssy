package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Network   NetworkConfig   `json:"network"`
	Shell     ShellConfig     `json:"shell"`
	Sandbox   SandboxConfig   `json:"sandbox"`
	Server    ServerConfig    `json:"server"`
	Engine    EngineConfig    `json:"engine"`
	Scheduler SchedulerConfig `json:"scheduler"`
	Output    OutputConfig    `json:"output"`
	Workspace WorkspaceConfig `json:"workspace"`
	Model     ModelConfig     `json:"model"`
	Providers ProvidersConfig `json:"providers"`
	Agents    AgentsConfig    `json:"agents"`
	Chat      ChatConfig      `json:"chat"`
	Discord   DiscordConfig   `json:"discord"`
	Telegram  TelegramConfig  `json:"telegram"`
	Secrets   SecretsConfig   `json:"secrets"`
	Memory    MemoryConfig    `json:"memory"`
	OpenClaw  OpenClawConfig  `json:"openclaw"`
}

const (
	ThinkingModeNever   = "never"
	ThinkingModeOnError = "on_error"
	ThinkingModeAlways  = "always"
)

type OutputConfig struct {
	ThinkingMode     string `json:"thinking_mode"`
	MaxThinkingChars int    `json:"max_thinking_chars,omitempty"`
}

func NormalizeThinkingMode(mode string) string {
	value := strings.ToLower(strings.TrimSpace(mode))
	if value == "" {
		return ThinkingModeNever
	}
	return value
}

func IsValidThinkingMode(mode string) bool {
	switch NormalizeThinkingMode(mode) {
	case ThinkingModeNever, ThinkingModeOnError, ThinkingModeAlways:
		return true
	default:
		return false
	}
}

type NetworkConfig struct {
	Enabled         bool     `json:"enabled"`
	AllowedDomains  []string `json:"allowed_domains,omitempty"`
	AllowLocalhosts bool     `json:"allow_localhosts,omitempty"`
}

type ShellConfig struct {
	EnableExec       bool     `json:"enable_exec"`
	AllowedCommands  []string `json:"allowed_commands,omitempty"`
	DefaultTimeoutMS int      `json:"default_timeout_ms,omitempty"`
	MaxTimeoutMS     int      `json:"max_timeout_ms,omitempty"`
}

type EngineConfig struct {
	MaxConcurrentRuns   int `json:"max_concurrent_runs,omitempty"`
	DefaultRunTimeoutMS int `json:"default_run_timeout_ms,omitempty"`
	MaxRunTimeoutMS     int `json:"max_run_timeout_ms,omitempty"`
}

type SchedulerConfig struct {
	CatchUp           bool `json:"catch_up"`
	MaxConcurrentJobs int  `json:"max_concurrent_jobs,omitempty"`
}

// DockerMountConfig describes a single bind mount for Docker containers.
type DockerMountConfig struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"readonly"`
}

// DockerSandboxConfig holds Docker-specific sandbox parameters.
// These are used by the Docker sandbox provider.
type DockerSandboxConfig struct {
	Image                  string              `json:"image"`
	Host                   string              `json:"host,omitempty"`
	NetworkEnabled         bool                `json:"network_enabled"`
	CPULimit               float64             `json:"cpu_limit,omitempty"`       // e.g. 0.5 = half CPU
	MemoryLimitMB          int                 `json:"memory_limit_mb,omitempty"` // e.g. 512
	Hardened               bool                `json:"hardened,omitempty"`
	RequireDedicatedDaemon bool                `json:"require_dedicated_daemon,omitempty"`
	AllowedImages          []string            `json:"allowed_images,omitempty"`
	PidsLimit              int                 `json:"pids_limit,omitempty"`
	ExtraEnv               []string            `json:"extra_env,omitempty"`
	Mounts                 []DockerMountConfig `json:"mounts,omitempty"`
	PullPolicy             string              `json:"pull_policy,omitempty"` // "always", "if-not-present", "never"
}

type SandboxConfig struct {
	Active   bool                `json:"active"`
	Provider string              `json:"provider"`
	Docker   DockerSandboxConfig `json:"docker,omitempty"`
}

type ServerConfig struct {
	BindAddress string `json:"bind_address"`
	Port        int    `json:"port"`
	TLSEnabled  bool   `json:"tls_enabled"`
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
	Dashboard   bool   `json:"dashboard_enabled"`
}

type WorkspaceConfig struct {
	Root string `json:"root"`
}

type ModelConfig struct {
	Provider    string  `json:"provider"`
	Name        string  `json:"name"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

type AgentProfile struct {
	Enabled         *bool       `json:"enabled,omitempty"`
	Model           ModelConfig `json:"model,omitempty"`
	SelfImprovement bool        `json:"self_improvement,omitempty"`
}

type AgentsConfig struct {
	EnabledAgentIDs          []string                `json:"enabled_agent_ids,omitempty"`
	AllowInterAgentMessaging bool                    `json:"allow_inter_agent_messaging"`
	AllowAgentModelOverrides bool                    `json:"allow_agent_model_overrides"`
	SelfImprovementEnabled   bool                    `json:"self_improvement_enabled"`
	Profiles                 map[string]AgentProfile `json:"profiles,omitempty"`
	// Delegation settings
	AutoDelegate           bool   `json:"auto_delegate,omitempty"`
	DelegationMode         string `json:"delegation_mode,omitempty"`
	DelegationThreshold    int    `json:"delegation_threshold,omitempty"`
	DelegationAgentID      string `json:"delegation_agent_id,omitempty"`
	DelegationCooldownIter int    `json:"delegation_cooldown_iterations,omitempty"`
}

type ProviderEndpointConfig struct {
	BaseURL   string            `json:"base_url"`
	APIKey    string            `json:"api_key,omitempty"`
	APIKeyEnv string            `json:"api_key_env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type ProvidersConfig struct {
	OpenAI     ProviderEndpointConfig `json:"openai"`
	OpenRouter ProviderEndpointConfig `json:"openrouter"`
	Requesty   ProviderEndpointConfig `json:"requesty"`
	ZAI        ProviderEndpointConfig `json:"zai"`
	Generic    ProviderEndpointConfig `json:"generic"`
}

type ChatConfig struct {
	Enabled               bool     `json:"enabled"`
	DefaultAgentID        string   `json:"default_agent_id"`
	AllowUsers            []string `json:"allow_users,omitempty"`
	AllowRooms            []string `json:"allow_rooms,omitempty"`
	RateLimitPerMin       int      `json:"rate_limit_per_min,omitempty"`
	GlobalRateLimitPerMin int      `json:"global_rate_limit_per_min,omitempty"`
}

type DiscordConfig struct {
	Enabled         bool     `json:"enabled"`
	Token           string   `json:"token,omitempty"`
	TokenEnv        string   `json:"token_env,omitempty"`
	DefaultAgentID  string   `json:"default_agent_id"`
	AllowGuilds     []string `json:"allow_guilds,omitempty"`
	AllowChannels   []string `json:"allow_channels,omitempty"`
	AllowUsers      []string `json:"allow_users,omitempty"`
	CommandPrefix   string   `json:"command_prefix,omitempty"`
	RateLimitPerMin int      `json:"rate_limit_per_min,omitempty"`
}

type TelegramConfig struct {
	Enabled         bool     `json:"enabled"`
	Token           string   `json:"token,omitempty"`
	TokenEnv        string   `json:"token_env,omitempty"`
	DefaultAgentID  string   `json:"default_agent_id"`
	AllowChats      []string `json:"allow_chats,omitempty"`
	AllowUsers      []string `json:"allow_users,omitempty"`
	CommandPrefix   string   `json:"command_prefix,omitempty"`
	RateLimitPerMin int      `json:"rate_limit_per_min,omitempty"`
}

type SecretsConfig struct {
	StoreFile     string `json:"store_file"`
	MasterKeyFile string `json:"master_key_file"`
}

type MemoryConfig struct {
	Enabled           bool   `json:"enabled"`
	MaxWorkingItems   int    `json:"max_working_items,omitempty"`
	MaxPromptTokens   int    `json:"max_prompt_tokens,omitempty"`
	AutoCheckpoint    bool   `json:"auto_checkpoint"`
	ProactiveEnabled  bool   `json:"proactive_enabled"`
	EmbeddingsEnabled bool   `json:"embeddings_enabled"`
	EmbeddingProvider string `json:"embedding_provider,omitempty"`
	EmbeddingModel    string `json:"embedding_model,omitempty"`
	EventBufferSize   int    `json:"event_buffer_size,omitempty"`
}

type OpenClawConfig struct {
	Remote OpenClawRemoteConfig `json:"remote"`
}

type OpenClawRemoteConfig struct {
	Enabled          bool   `json:"enabled"`
	RepositoryURL    string `json:"repository_url"`
	BinaryPath       string `json:"binary_path"`
	WSPrimary        string `json:"ws_primary"`
	WSFallback       string `json:"ws_fallback,omitempty"`
	SessionKey       string `json:"session_key"`
	ConnectTimeoutMS int    `json:"connect_timeout_ms,omitempty"`
	RequestTimeoutMS int    `json:"request_timeout_ms,omitempty"`
	PollIntervalMS   int    `json:"poll_interval_ms,omitempty"`
	PollTimeoutMS    int    `json:"poll_timeout_ms,omitempty"`
	PreferTailnetWSS bool   `json:"prefer_tailnet_wss"`
}

func Default() Config {
	return Config{
		Network: NetworkConfig{
			Enabled: false,
		},
		Shell: ShellConfig{
			EnableExec: false,
		},
		Sandbox: SandboxConfig{
			Active:   false,
			Provider: "none",
			Docker: DockerSandboxConfig{
				Image:         "ubuntu:24.04",
				PullPolicy:    "if-not-present",
				CPULimit:      1.0,
				MemoryLimitMB: 2048,
			},
		},
		Engine: EngineConfig{
			MaxConcurrentRuns:   64,
			DefaultRunTimeoutMS: 20 * 60 * 1000,
			MaxRunTimeoutMS:     2 * 60 * 60 * 1000,
		},
		Scheduler: SchedulerConfig{
			CatchUp:           true,
			MaxConcurrentJobs: 4,
		},
		Server: ServerConfig{
			BindAddress: "127.0.0.1",
			Port:        8080,
			TLSEnabled:  false,
			TLSCertFile: ".openclawssy/certs/server.crt",
			TLSKeyFile:  ".openclawssy/certs/server.key",
			Dashboard:   true,
		},
		Output: OutputConfig{
			ThinkingMode:     ThinkingModeNever,
			MaxThinkingChars: 4000,
		},
		Workspace: WorkspaceConfig{
			Root: "./workspace",
		},
		Model: ModelConfig{
			Provider:    "zai",
			Name:        "GLM-4.7",
			Temperature: 0.2,
			MaxTokens:   20000,
		},
		Providers: ProvidersConfig{
			OpenAI: ProviderEndpointConfig{
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
			},
			OpenRouter: ProviderEndpointConfig{
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
			},
			Requesty: ProviderEndpointConfig{
				BaseURL:   "https://router.requesty.ai/v1",
				APIKeyEnv: "REQUESTY_API_KEY",
			},
			ZAI: ProviderEndpointConfig{
				BaseURL:   "https://api.z.ai/api/coding/paas/v4",
				APIKeyEnv: "ZAI_API_KEY",
			},
			Generic: ProviderEndpointConfig{
				BaseURL:   "",
				APIKeyEnv: "OPENAI_COMPAT_API_KEY",
			},
		},
		Agents: AgentsConfig{
			AllowInterAgentMessaging: true,
			AllowAgentModelOverrides: true,
			SelfImprovementEnabled:   false,
			Profiles:                 map[string]AgentProfile{},
			AutoDelegate:             false,
			DelegationMode:           "tool_gated",
			DelegationThreshold:      2,
			DelegationAgentID:        "default",
			DelegationCooldownIter:   15,
		},
		Chat: ChatConfig{
			Enabled:               true,
			DefaultAgentID:        "default",
			AllowUsers:            []string{"dashboard_user"},
			RateLimitPerMin:       20,
			GlobalRateLimitPerMin: 120,
		},
		Discord: DiscordConfig{
			Enabled:         false,
			TokenEnv:        "DISCORD_BOT_TOKEN",
			DefaultAgentID:  "default",
			CommandPrefix:   "!ask",
			RateLimitPerMin: 20,
		},
		Telegram: TelegramConfig{
			Enabled:         false,
			TokenEnv:        "TELEGRAM_BOT_TOKEN",
			DefaultAgentID:  "default",
			CommandPrefix:   "/ask",
			RateLimitPerMin: 20,
		},
		Secrets: SecretsConfig{
			StoreFile:     ".openclawssy/secrets.enc",
			MasterKeyFile: ".openclawssy/master.key",
		},
		Memory: MemoryConfig{
			Enabled:           false,
			MaxWorkingItems:   200,
			MaxPromptTokens:   1200,
			AutoCheckpoint:    false,
			ProactiveEnabled:  true,
			EmbeddingsEnabled: false,
			EmbeddingProvider: "openrouter",
			EmbeddingModel:    "text-embedding-3-small",
			EventBufferSize:   256,
		},
		OpenClaw: OpenClawConfig{
			Remote: OpenClawRemoteConfig{
				Enabled:          false,
				RepositoryURL:    "https://github.com/mojomast/openclawremoteussy.git",
				BinaryPath:       "openclawremoteussy",
				WSPrimary:        "wss://kimi.tailec998.ts.net",
				WSFallback:       "ws://100.125.104.79:18789",
				SessionKey:       "agent:main:main",
				ConnectTimeoutMS: 10000,
				RequestTimeoutMS: 15000,
				PollIntervalMS:   1200,
				PollTimeoutMS:    60000,
				PreferTailnetWSS: true,
			},
		},
	}
}

func (c *Config) ApplyDefaults() {
	d := Default()
	if c.Sandbox.Provider == "" {
		if c.Sandbox.Active {
			c.Sandbox.Provider = "docker"
		} else {
			c.Sandbox.Provider = d.Sandbox.Provider
		}
	}
	if c.Sandbox.Docker.Image == "" {
		c.Sandbox.Docker.Image = d.Sandbox.Docker.Image
	}
	if c.Sandbox.Docker.PullPolicy == "" {
		c.Sandbox.Docker.PullPolicy = d.Sandbox.Docker.PullPolicy
	}
	if c.Sandbox.Docker.CPULimit <= 0 {
		c.Sandbox.Docker.CPULimit = d.Sandbox.Docker.CPULimit
	}
	if c.Sandbox.Docker.MemoryLimitMB <= 0 {
		c.Sandbox.Docker.MemoryLimitMB = d.Sandbox.Docker.MemoryLimitMB
	}
	if c.Sandbox.Docker.Hardened && c.Sandbox.Docker.PidsLimit <= 0 {
		c.Sandbox.Docker.PidsLimit = 256
	}
	if c.Server.BindAddress == "" {
		c.Server.BindAddress = d.Server.BindAddress
	}
	if c.Server.Port == 0 {
		c.Server.Port = d.Server.Port
	}
	if c.Workspace.Root == "" {
		c.Workspace.Root = d.Workspace.Root
	}
	if strings.TrimSpace(c.Output.ThinkingMode) == "" {
		c.Output.ThinkingMode = d.Output.ThinkingMode
	} else {
		c.Output.ThinkingMode = NormalizeThinkingMode(c.Output.ThinkingMode)
	}
	if c.Output.MaxThinkingChars <= 0 {
		c.Output.MaxThinkingChars = d.Output.MaxThinkingChars
	}
	if c.Engine.MaxConcurrentRuns <= 0 {
		c.Engine.MaxConcurrentRuns = d.Engine.MaxConcurrentRuns
	}
	if c.Scheduler.MaxConcurrentJobs <= 0 {
		c.Scheduler.MaxConcurrentJobs = d.Scheduler.MaxConcurrentJobs
	}
	if c.Server.TLSCertFile == "" {
		c.Server.TLSCertFile = d.Server.TLSCertFile
	}
	if c.Server.TLSKeyFile == "" {
		c.Server.TLSKeyFile = d.Server.TLSKeyFile
	}
	if c.Model.Provider == "" {
		c.Model.Provider = d.Model.Provider
	}
	if c.Model.Name == "" {
		c.Model.Name = d.Model.Name
	}
	if c.Model.MaxTokens == 0 {
		c.Model.MaxTokens = d.Model.MaxTokens
	}
	if c.Chat.DefaultAgentID == "" {
		c.Chat.DefaultAgentID = d.Chat.DefaultAgentID
	}
	if len(c.Agents.Profiles) == 0 && len(d.Agents.Profiles) == 0 {
		c.Agents.Profiles = map[string]AgentProfile{}
	}
	// Delegation defaults
	if c.Agents.DelegationMode == "" {
		c.Agents.DelegationMode = d.Agents.DelegationMode
	}
	if c.Agents.DelegationThreshold == 0 {
		c.Agents.DelegationThreshold = d.Agents.DelegationThreshold
	}
	if c.Agents.DelegationAgentID == "" {
		c.Agents.DelegationAgentID = d.Agents.DelegationAgentID
	}
	if c.Agents.DelegationCooldownIter == 0 {
		c.Agents.DelegationCooldownIter = d.Agents.DelegationCooldownIter
	}
	if c.Chat.RateLimitPerMin == 0 {
		c.Chat.RateLimitPerMin = d.Chat.RateLimitPerMin
	}
	if c.Chat.GlobalRateLimitPerMin == 0 {
		c.Chat.GlobalRateLimitPerMin = d.Chat.GlobalRateLimitPerMin
	}
	if c.Discord.TokenEnv == "" {
		c.Discord.TokenEnv = d.Discord.TokenEnv
	}
	if c.Discord.DefaultAgentID == "" {
		c.Discord.DefaultAgentID = d.Discord.DefaultAgentID
	}
	if c.Discord.CommandPrefix == "" {
		c.Discord.CommandPrefix = d.Discord.CommandPrefix
	}
	if c.Discord.RateLimitPerMin == 0 {
		c.Discord.RateLimitPerMin = d.Discord.RateLimitPerMin
	}
	if c.Telegram.TokenEnv == "" {
		c.Telegram.TokenEnv = d.Telegram.TokenEnv
	}
	if c.Telegram.DefaultAgentID == "" {
		c.Telegram.DefaultAgentID = d.Telegram.DefaultAgentID
	}
	if c.Telegram.RateLimitPerMin == 0 {
		c.Telegram.RateLimitPerMin = d.Telegram.RateLimitPerMin
	}
	if c.Secrets.StoreFile == "" {
		c.Secrets.StoreFile = d.Secrets.StoreFile
	}
	if c.Secrets.MasterKeyFile == "" {
		c.Secrets.MasterKeyFile = d.Secrets.MasterKeyFile
	}
	if c.Memory.MaxWorkingItems <= 0 {
		c.Memory.MaxWorkingItems = d.Memory.MaxWorkingItems
	}
	if c.Memory.MaxPromptTokens <= 0 {
		c.Memory.MaxPromptTokens = d.Memory.MaxPromptTokens
	}
	if c.Memory.EventBufferSize <= 0 {
		c.Memory.EventBufferSize = d.Memory.EventBufferSize
	}
	if strings.TrimSpace(c.Memory.EmbeddingProvider) == "" {
		c.Memory.EmbeddingProvider = d.Memory.EmbeddingProvider
	}
	if strings.TrimSpace(c.Memory.EmbeddingModel) == "" {
		c.Memory.EmbeddingModel = d.Memory.EmbeddingModel
	}
	if strings.TrimSpace(c.OpenClaw.Remote.WSPrimary) == "" {
		c.OpenClaw.Remote.WSPrimary = d.OpenClaw.Remote.WSPrimary
	}
	if strings.TrimSpace(c.OpenClaw.Remote.RepositoryURL) == "" {
		c.OpenClaw.Remote.RepositoryURL = d.OpenClaw.Remote.RepositoryURL
	}
	if strings.TrimSpace(c.OpenClaw.Remote.BinaryPath) == "" {
		c.OpenClaw.Remote.BinaryPath = d.OpenClaw.Remote.BinaryPath
	}
	if strings.TrimSpace(c.OpenClaw.Remote.WSFallback) == "" {
		c.OpenClaw.Remote.WSFallback = d.OpenClaw.Remote.WSFallback
	}
	if strings.TrimSpace(c.OpenClaw.Remote.SessionKey) == "" {
		c.OpenClaw.Remote.SessionKey = d.OpenClaw.Remote.SessionKey
	}
	if c.OpenClaw.Remote.ConnectTimeoutMS <= 0 {
		c.OpenClaw.Remote.ConnectTimeoutMS = d.OpenClaw.Remote.ConnectTimeoutMS
	}
	if c.OpenClaw.Remote.RequestTimeoutMS <= 0 {
		c.OpenClaw.Remote.RequestTimeoutMS = d.OpenClaw.Remote.RequestTimeoutMS
	}
	if c.OpenClaw.Remote.PollIntervalMS <= 0 {
		c.OpenClaw.Remote.PollIntervalMS = d.OpenClaw.Remote.PollIntervalMS
	}
	if c.OpenClaw.Remote.PollTimeoutMS <= 0 {
		c.OpenClaw.Remote.PollTimeoutMS = d.OpenClaw.Remote.PollTimeoutMS
	}

	if c.Providers.OpenAI.BaseURL == "" {
		c.Providers.OpenAI = d.Providers.OpenAI
	}
	if c.Providers.OpenRouter.BaseURL == "" {
		c.Providers.OpenRouter = d.Providers.OpenRouter
	}
	if c.Providers.Requesty.BaseURL == "" {
		c.Providers.Requesty = d.Providers.Requesty
	}
	if c.Providers.ZAI.BaseURL == "" {
		c.Providers.ZAI = d.Providers.ZAI
	}
	if c.Providers.Generic.APIKeyEnv == "" && c.Providers.Generic.APIKey == "" {
		c.Providers.Generic.APIKeyEnv = d.Providers.Generic.APIKeyEnv
	}
}

func (c Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port out of range: %d", c.Server.Port)
	}

	host := c.Server.BindAddress
	if host == "" {
		return errors.New("server.bind_address is required")
	}
	if ip := net.ParseIP(host); ip == nil {
		return fmt.Errorf("server.bind_address must be an IP address: %q", host)
	}

	sandboxProvider := strings.ToLower(strings.TrimSpace(c.Sandbox.Provider))
	allowedSandboxProviders := map[string]bool{"none": true, "local": true, "docker": true}
	if !allowedSandboxProviders[sandboxProvider] {
		return fmt.Errorf("unsupported sandbox provider: %q", c.Sandbox.Provider)
	}

	if c.Shell.EnableExec && !c.Sandbox.Active {
		return errors.New("shell.enable_exec cannot be true when sandbox.active is false")
	}
	if c.Sandbox.Active && sandboxProvider == "none" {
		return errors.New("sandbox.provider must not be 'none' when sandbox.active is true")
	}
	if c.Sandbox.Docker.RequireDedicatedDaemon {
		host := strings.TrimSpace(c.Sandbox.Docker.Host)
		if host == "" {
			return errors.New("sandbox.docker.require_dedicated_daemon=true requires sandbox.docker.host")
		}
		if host == "unix:///var/run/docker.sock" {
			return errors.New("sandbox.docker.host must not use the default host socket when require_dedicated_daemon=true")
		}
	}
	dockerHost := strings.TrimSpace(c.Sandbox.Docker.Host)
	if dockerHost != "" {
		if !strings.HasPrefix(dockerHost, "unix://") && !strings.HasPrefix(dockerHost, "tcp://") && !strings.HasPrefix(dockerHost, "ssh://") {
			return errors.New("sandbox.docker.host must start with unix://, tcp://, or ssh://")
		}
	}
	for _, img := range c.Sandbox.Docker.AllowedImages {
		if strings.TrimSpace(img) == "" {
			return errors.New("sandbox.docker.allowed_images cannot contain empty entries")
		}
	}
	if c.Sandbox.Docker.PidsLimit < 0 {
		return errors.New("sandbox.docker.pids_limit must be >= 0")
	}
	if !c.Sandbox.Active && c.Shell.EnableExec {
		return errors.New("shell execution requires active sandbox")
	}
	for _, cmd := range c.Shell.AllowedCommands {
		if strings.TrimSpace(cmd) == "" {
			return errors.New("shell.allowed_commands cannot contain empty entries")
		}
	}

	if strings.TrimSpace(c.Workspace.Root) == "" {
		return errors.New("workspace.root cannot be empty")
	}
	for _, agentID := range c.Agents.EnabledAgentIDs {
		if err := validateAgentID(agentID); err != nil {
			return fmt.Errorf("agents.enabled_agent_ids: %w", err)
		}
	}
	for agentID, profile := range c.Agents.Profiles {
		if err := validateAgentID(agentID); err != nil {
			return fmt.Errorf("agents.profiles.%s: %w", agentID, err)
		}
		if strings.TrimSpace(profile.Model.Provider) != "" {
			provider := strings.ToLower(strings.TrimSpace(profile.Model.Provider))
			supported := map[string]bool{
				"openai": true, "openrouter": true, "requesty": true, "zai": true, "generic": true,
			}
			if !supported[provider] {
				return fmt.Errorf("agents.profiles.%s.model.provider unsupported: %q", agentID, profile.Model.Provider)
			}
		}
		if profile.Model.MaxTokens < 0 || profile.Model.MaxTokens > 20000 {
			return fmt.Errorf("agents.profiles.%s.model.max_tokens must be between 0 and 20000", agentID)
		}
	}

	if !IsValidThinkingMode(c.Output.ThinkingMode) {
		return fmt.Errorf("output.thinking_mode must be one of never|on_error|always")
	}
	if c.Output.MaxThinkingChars < 64 || c.Output.MaxThinkingChars > 100000 {
		return errors.New("output.max_thinking_chars must be between 64 and 100000")
	}
	if c.Engine.MaxConcurrentRuns < 1 || c.Engine.MaxConcurrentRuns > 10000 {
		return errors.New("engine.max_concurrent_runs must be between 1 and 10000")
	}
	if c.Engine.DefaultRunTimeoutMS < 0 {
		return errors.New("engine.default_run_timeout_ms must be >= 0")
	}
	if c.Engine.MaxRunTimeoutMS < 0 {
		return errors.New("engine.max_run_timeout_ms must be >= 0")
	}
	if c.Engine.DefaultRunTimeoutMS > 0 && (c.Engine.DefaultRunTimeoutMS < 1000 || c.Engine.DefaultRunTimeoutMS > 24*60*60*1000) {
		return errors.New("engine.default_run_timeout_ms must be between 1000 and 86400000 when set")
	}
	if c.Engine.MaxRunTimeoutMS > 0 && (c.Engine.MaxRunTimeoutMS < 1000 || c.Engine.MaxRunTimeoutMS > 24*60*60*1000) {
		return errors.New("engine.max_run_timeout_ms must be between 1000 and 86400000 when set")
	}
	if c.Engine.MaxRunTimeoutMS > 0 && c.Engine.DefaultRunTimeoutMS > 0 && c.Engine.DefaultRunTimeoutMS > c.Engine.MaxRunTimeoutMS {
		return errors.New("engine.default_run_timeout_ms cannot exceed engine.max_run_timeout_ms")
	}
	if c.Scheduler.MaxConcurrentJobs < 1 || c.Scheduler.MaxConcurrentJobs > 1000 {
		return errors.New("scheduler.max_concurrent_jobs must be between 1 and 1000")
	}

	for _, d := range c.Network.AllowedDomains {
		d = strings.TrimSpace(d)
		if d == "" {
			return errors.New("network.allowed_domains cannot contain empty entries")
		}
		if strings.Contains(d, " ") {
			return fmt.Errorf("invalid allowed domain: %q", d)
		}
	}

	provider := strings.ToLower(strings.TrimSpace(c.Model.Provider))
	supported := map[string]bool{
		"openai": true, "openrouter": true, "requesty": true, "zai": true, "generic": true,
	}
	if !supported[provider] {
		return fmt.Errorf("unsupported model provider: %q", c.Model.Provider)
	}
	if strings.TrimSpace(c.Model.Name) == "" {
		return errors.New("model.name is required")
	}
	if c.Model.MaxTokens < 1 || c.Model.MaxTokens > 20000 {
		return errors.New("model.max_tokens must be between 1 and 20000")
	}
	if c.Chat.RateLimitPerMin < 1 {
		return errors.New("chat.rate_limit_per_min must be >= 1")
	}
	if c.Chat.GlobalRateLimitPerMin < 1 {
		return errors.New("chat.global_rate_limit_per_min must be >= 1")
	}
	if c.Discord.RateLimitPerMin < 1 {
		return errors.New("discord.rate_limit_per_min must be >= 1")
	}
	if c.Telegram.RateLimitPerMin < 1 {
		return errors.New("telegram.rate_limit_per_min must be >= 1")
	}
	if c.Server.TLSEnabled {
		if strings.TrimSpace(c.Server.TLSCertFile) == "" || strings.TrimSpace(c.Server.TLSKeyFile) == "" {
			return errors.New("tls requires server.tls_cert_file and server.tls_key_file")
		}
	}
	if strings.TrimSpace(c.Secrets.StoreFile) == "" || strings.TrimSpace(c.Secrets.MasterKeyFile) == "" {
		return errors.New("secrets.store_file and secrets.master_key_file are required")
	}
	if c.Memory.MaxWorkingItems < 1 || c.Memory.MaxWorkingItems > 100000 {
		return errors.New("memory.max_working_items must be between 1 and 100000")
	}
	if c.Memory.MaxPromptTokens < 64 || c.Memory.MaxPromptTokens > 100000 {
		return errors.New("memory.max_prompt_tokens must be between 64 and 100000")
	}
	if c.Memory.EventBufferSize < 1 || c.Memory.EventBufferSize > 10000 {
		return errors.New("memory.event_buffer_size must be between 1 and 10000")
	}
	embeddingProvider := strings.ToLower(strings.TrimSpace(c.Memory.EmbeddingProvider))
	supportedEmbeddingProviders := map[string]bool{"openai": true, "openrouter": true, "requesty": true, "zai": true, "generic": true}
	if !supportedEmbeddingProviders[embeddingProvider] {
		return fmt.Errorf("unsupported memory.embedding_provider: %q", c.Memory.EmbeddingProvider)
	}
	if strings.TrimSpace(c.Memory.EmbeddingModel) == "" {
		return errors.New("memory.embedding_model is required")
	}
	if err := validateWebSocketURL(c.OpenClaw.Remote.WSPrimary, "openclaw.remote.ws_primary"); err != nil {
		return err
	}
	if fallback := strings.TrimSpace(c.OpenClaw.Remote.WSFallback); fallback != "" {
		if err := validateWebSocketURL(fallback, "openclaw.remote.ws_fallback"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.OpenClaw.Remote.SessionKey) == "" {
		return errors.New("openclaw.remote.session_key is required")
	}
	if strings.TrimSpace(c.OpenClaw.Remote.RepositoryURL) == "" {
		return errors.New("openclaw.remote.repository_url is required")
	}
	if strings.TrimSpace(c.OpenClaw.Remote.BinaryPath) == "" {
		return errors.New("openclaw.remote.binary_path is required")
	}
	if c.OpenClaw.Remote.ConnectTimeoutMS < 1000 || c.OpenClaw.Remote.ConnectTimeoutMS > 120000 {
		return errors.New("openclaw.remote.connect_timeout_ms must be between 1000 and 120000")
	}
	if c.OpenClaw.Remote.RequestTimeoutMS < 1000 || c.OpenClaw.Remote.RequestTimeoutMS > 120000 {
		return errors.New("openclaw.remote.request_timeout_ms must be between 1000 and 120000")
	}
	if c.OpenClaw.Remote.PollIntervalMS < 100 || c.OpenClaw.Remote.PollIntervalMS > 60000 {
		return errors.New("openclaw.remote.poll_interval_ms must be between 100 and 60000")
	}
	if c.OpenClaw.Remote.PollTimeoutMS < 1000 || c.OpenClaw.Remote.PollTimeoutMS > 600000 {
		return errors.New("openclaw.remote.poll_timeout_ms must be between 1000 and 600000")
	}
	if c.OpenClaw.Remote.PollTimeoutMS < c.OpenClaw.Remote.PollIntervalMS {
		return errors.New("openclaw.remote.poll_timeout_ms cannot be less than openclaw.remote.poll_interval_ms")
	}

	return nil
}

func validateWebSocketURL(raw, field string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%s invalid: %w", field, err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "ws" && scheme != "wss" {
		return fmt.Errorf("%s must use ws:// or wss://", field)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("%s host is required", field)
	}
	return nil
}

func validateAgentID(raw string) error {
	agentID := strings.TrimSpace(raw)
	if agentID == "" {
		return errors.New("agent id cannot be empty")
	}
	if strings.Contains(agentID, "..") || strings.ContainsRune(agentID, '/') || strings.ContainsRune(agentID, '\\') {
		return fmt.Errorf("invalid agent id: %q", raw)
	}
	return nil
}

func (c Config) Redacted() Config {
	redacted := c
	redacted.Providers.OpenAI.APIKey = ""
	redacted.Providers.OpenRouter.APIKey = ""
	redacted.Providers.Requesty.APIKey = ""
	redacted.Providers.ZAI.APIKey = ""
	redacted.Providers.Generic.APIKey = ""
	redacted.Discord.Token = ""
	redacted.Telegram.Token = ""
	return redacted
}

func LoadOrDefault(path string) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			return cfg, nil
		}
		return Config{}, err
	}
	return Load(path)
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	buf = append(buf, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return WriteAtomic(path, buf, 0o600)
}
