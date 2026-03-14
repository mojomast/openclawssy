package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	InheritanceSourceGlobal           = "global"
	InheritanceSourceAgentProfile     = "agent-profile"
	InheritanceSourceSubagentOverride = "subagent-override"
)

var (
	validDelegationModes = map[string]struct{}{
		"":                {},
		"prompt_only":     {},
		"tool_gated":      {},
		"auto_execute":    {},
		"suggest_only":    {},
		"approve_plan":    {},
		"auto_trusted":    {},
		"full_autonomous": {},
	}

	validInheritanceSources = map[string]struct{}{
		InheritanceSourceGlobal:           {},
		InheritanceSourceAgentProfile:     {},
		InheritanceSourceSubagentOverride: {},
	}

	requiredTopLevelInheritanceKeys = []string{
		"identity",
		"mission",
		"system_prompt",
		"tool_policy",
		"delegation_policy",
		"memory_policy",
		"model_policy",
		"sandbox_policy",
		"observability_policy",
	}
)

type AgentContract struct {
	Identity            Identity            `json:"identity"`
	Mission             Mission             `json:"mission"`
	SystemPrompt        SystemPrompt        `json:"system_prompt"`
	ToolPolicy          ToolPolicy          `json:"tool_policy"`
	DelegationPolicy    DelegationPolicy    `json:"delegation_policy"`
	MemoryPolicy        MemoryPolicy        `json:"memory_policy"`
	ModelPolicy         ModelPolicy         `json:"model_policy"`
	SandboxPolicy       SandboxPolicy       `json:"sandbox_policy"`
	ObservabilityPolicy ObservabilityPolicy `json:"observability_policy"`
	Inheritance         Inheritance         `json:"inheritance"`
}

type Identity struct {
	AgentID     string `json:"agent_id"`
	DisplayName string `json:"display_name"`
}

type Mission struct {
	Description string   `json:"description"`
	Goals       []string `json:"goals,omitempty"`
}

type SystemPrompt struct {
	Content string `json:"content"`
	Source  string `json:"source"`
}

type ToolPolicy struct {
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"`
	DefaultDeny  bool     `json:"default_deny"`
}

type DelegationPolicy struct {
	Mode         string `json:"mode"`
	Threshold    int    `json:"threshold"`
	Cooldown     int    `json:"cooldown"`
	AutoDelegate bool   `json:"auto_delegate"`
	AgentID      string `json:"agent_id"`
	MaxDepth     int    `json:"max_depth"`
}

type MemoryPolicy struct {
	Enabled         bool `json:"enabled"`
	MaxWorkingItems int  `json:"max_working_items"`
	MaxPromptTokens int  `json:"max_prompt_tokens"`
	AutoCheckpoint  bool `json:"auto_checkpoint"`
	Proactive       bool `json:"proactive"`
	Embeddings      bool `json:"embeddings"`
}

type ModelPolicy struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	TimeoutMS   int     `json:"timeout_ms"`
}

type SandboxPolicy struct {
	Active   bool                `json:"active"`
	Provider string              `json:"provider"`
	Docker   SandboxDockerConfig `json:"docker"`
}

type DockerMount struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"readonly"`
}

type SandboxDockerConfig struct {
	Image                  string        `json:"image"`
	Host                   string        `json:"host,omitempty"`
	NetworkEnabled         bool          `json:"network_enabled"`
	CPULimit               float64       `json:"cpu_limit"`
	MemoryLimitMB          int           `json:"memory_limit_mb"`
	Hardened               bool          `json:"hardened,omitempty"`
	RequireDedicatedDaemon bool          `json:"require_dedicated_daemon,omitempty"`
	AllowedImages          []string      `json:"allowed_images,omitempty"`
	PidsLimit              int           `json:"pids_limit,omitempty"`
	ExtraEnv               []string      `json:"extra_env,omitempty"`
	Mounts                 []DockerMount `json:"mounts,omitempty"`
	PullPolicy             string        `json:"pull_policy,omitempty"`
}

type ObservabilityPolicy struct {
	AuditEnabled bool   `json:"audit_enabled"`
	TraceEnabled bool   `json:"trace_enabled"`
	ThinkingMode string `json:"thinking_mode"`
}

type Inheritance struct {
	Source map[string]string `json:"source"`
}

func (c AgentContract) Validate() error {
	if strings.TrimSpace(c.Identity.AgentID) == "" {
		return errors.New("identity.agent_id cannot be empty")
	}

	mode := strings.TrimSpace(strings.ToLower(c.DelegationPolicy.Mode))
	if _, ok := validDelegationModes[mode]; !ok {
		modes := make([]string, 0, len(validDelegationModes)-1)
		for candidate := range validDelegationModes {
			if candidate == "" {
				continue
			}
			modes = append(modes, candidate)
		}
		sort.Strings(modes)
		return fmt.Errorf("delegation mode must be one of %s, got %q", strings.Join(modes, "|"), c.DelegationPolicy.Mode)
	}

	if c.DelegationPolicy.Threshold < 0 {
		return fmt.Errorf("delegation_policy.threshold must be >= 0, got %d", c.DelegationPolicy.Threshold)
	}
	if c.DelegationPolicy.Cooldown < 0 {
		return fmt.Errorf("delegation_policy.cooldown must be >= 0, got %d", c.DelegationPolicy.Cooldown)
	}
	if c.DelegationPolicy.MaxDepth < 0 {
		return fmt.Errorf("delegation_policy.max_depth must be >= 0, got %d", c.DelegationPolicy.MaxDepth)
	}
	if c.MemoryPolicy.MaxWorkingItems < 0 {
		return fmt.Errorf("memory_policy.max_working_items must be >= 0, got %d", c.MemoryPolicy.MaxWorkingItems)
	}
	if c.MemoryPolicy.MaxPromptTokens < 0 {
		return fmt.Errorf("memory_policy.max_prompt_tokens must be >= 0, got %d", c.MemoryPolicy.MaxPromptTokens)
	}
	if c.ModelPolicy.MaxTokens < 0 {
		return fmt.Errorf("model_policy.max_tokens must be >= 0, got %d", c.ModelPolicy.MaxTokens)
	}
	if c.ModelPolicy.TimeoutMS < 0 {
		return fmt.Errorf("model_policy.timeout_ms must be >= 0, got %d", c.ModelPolicy.TimeoutMS)
	}
	if c.SandboxPolicy.Docker.CPULimit < 0 {
		return fmt.Errorf("sandbox_policy.docker.cpu_limit must be >= 0, got %f", c.SandboxPolicy.Docker.CPULimit)
	}
	if c.SandboxPolicy.Docker.MemoryLimitMB < 0 {
		return fmt.Errorf("sandbox_policy.docker.memory_limit_mb must be >= 0, got %d", c.SandboxPolicy.Docker.MemoryLimitMB)
	}

	if len(c.Inheritance.Source) == 0 {
		return errors.New("inheritance.source cannot be empty")
	}
	for _, key := range requiredTopLevelInheritanceKeys {
		source, ok := c.Inheritance.Source[key]
		if !ok {
			return fmt.Errorf("inheritance.source missing key %q", key)
		}
		if _, valid := validInheritanceSources[strings.TrimSpace(source)]; !valid {
			return fmt.Errorf("inheritance.source[%q] must be one of global|agent-profile|subagent-override, got %q", key, source)
		}
	}
	for field, source := range c.Inheritance.Source {
		if strings.TrimSpace(field) == "" {
			return errors.New("inheritance.source contains empty field key")
		}
		if _, valid := validInheritanceSources[strings.TrimSpace(source)]; !valid {
			return fmt.Errorf("inheritance.source[%q] must be one of global|agent-profile|subagent-override, got %q", field, source)
		}
	}

	return nil
}

func (c AgentContract) String() string {
	encoded, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Sprintf("AgentContract{marshal_error:%q}", err.Error())
	}
	return string(encoded)
}
