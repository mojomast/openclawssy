package instances

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/promptstack"
	"openclawssy/internal/roles"
)

const (
	DefaultInstanceID        = "default"
	PromptSourcePromptStack  = "prompt_stack"
	PromptSourceMigratedDocs = "migrated_from_docs"
	PromptSourceLegacyDocs   = "legacy_docs"
)

type FeatureFlagState struct {
	Enabled  bool `json:"enabled"`
	ReadOnly bool `json:"read_only"`
	Visible  bool `json:"visible"`
}

type FeatureSet struct {
	AgentContract  FeatureFlagState `json:"agent_contract"`
	PromptStack    FeatureFlagState `json:"prompt_stack"`
	RoleTemplates  FeatureFlagState `json:"role_templates"`
	Delegation     FeatureFlagState `json:"delegation"`
	Eval           FeatureFlagState `json:"eval"`
	Instances      FeatureFlagState `json:"instances"`
	AgentMessaging FeatureFlagState `json:"agent_messaging"`
}

type InstanceSettings struct {
	ActiveInstanceID                 string `json:"active_instance_id,omitempty"`
	AllowParallelInstances           bool   `json:"allow_parallel_instances"`
	DefaultMaxConcurrentInstanceRuns int    `json:"default_max_concurrent_instance_runs,omitempty"`
}

type WorkspaceConfig struct {
	Root   string `json:"root"`
	Shared bool   `json:"shared"`
}

type RuntimeConfig struct {
	DefaultAgentID    string   `json:"default_agent_id"`
	EnabledAgentIDs   []string `json:"enabled_agent_ids,omitempty"`
	MaxConcurrentRuns int      `json:"max_concurrent_runs,omitempty"`
}

type PromptingConfig struct {
	SourceMode      string `json:"source_mode"`
	MaterializeDocs bool   `json:"materialize_docs"`
}

type DelegationPolicy struct {
	Enabled            bool   `json:"enabled"`
	Mode               string `json:"mode,omitempty"`
	Threshold          int    `json:"threshold,omitempty"`
	CooldownIterations int    `json:"cooldown_iterations,omitempty"`
	MaxDepth           int    `json:"max_depth,omitempty"`
	DelegationAgentID  string `json:"delegation_agent_id,omitempty"`
	DefaultRole        string `json:"default_role,omitempty"`
}

type MessagingPolicy struct {
	Enabled                  bool   `json:"enabled"`
	AllowInterAgentMessaging bool   `json:"allow_inter_agent_messaging"`
	SharedInboxNamespace     string `json:"shared_inbox_namespace,omitempty"`
	AllowCrossInstance       bool   `json:"allow_cross_instance"`
}

type SkillsConfig struct {
	Activated []string `json:"activated,omitempty"`
}

type RolesConfig struct {
	TemplateNames []string `json:"template_names,omitempty"`
}

type ChannelRoute struct {
	DefaultAgentID string `json:"default_agent_id,omitempty"`
}

type InstanceManifest struct {
	InstanceID  string                  `json:"instance_id"`
	DisplayName string                  `json:"display_name,omitempty"`
	Description string                  `json:"description,omitempty"`
	Enabled     bool                    `json:"enabled"`
	Workspace   WorkspaceConfig         `json:"workspace"`
	Runtime     RuntimeConfig           `json:"runtime"`
	Prompting   PromptingConfig         `json:"prompting"`
	Delegation  DelegationPolicy        `json:"delegation"`
	Messaging   MessagingPolicy         `json:"messaging"`
	Skills      SkillsConfig            `json:"skills"`
	Roles       RolesConfig             `json:"roles"`
	Channels    map[string]ChannelRoute `json:"channels,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

type AgentIdentity struct {
	AssistantName string `json:"assistant_name,omitempty"`
	UserName      string `json:"user_name,omitempty"`
}

type AgentBehavior struct {
	SelfImprovement     bool   `json:"self_improvement"`
	DefaultThinkingMode string `json:"default_thinking_mode,omitempty"`
}

type AgentRestrictions struct {
	AllowedTools      []string `json:"allowed_tools,omitempty"`
	MaxToolIterations int      `json:"max_tool_iterations,omitempty"`
	TimeoutMS         int      `json:"timeout_ms,omitempty"`
	MaxConcurrentRuns int      `json:"max_concurrent_runs,omitempty"`
}

type AgentCommunication struct {
	CanMessage         []string `json:"can_message,omitempty"`
	CanReceiveFrom     []string `json:"can_receive_from,omitempty"`
	AllowCrossInstance bool     `json:"allow_cross_instance"`
}

type AgentWorkspace struct {
	OverlayRoot string `json:"overlay_root,omitempty"`
}

type AgentManifest struct {
	AgentID          string             `json:"agent_id"`
	DisplayName      string             `json:"display_name,omitempty"`
	Enabled          bool               `json:"enabled"`
	Identity         AgentIdentity      `json:"identity,omitempty"`
	Model            config.ModelConfig `json:"model,omitempty"`
	Behavior         AgentBehavior      `json:"behavior,omitempty"`
	Delegation       DelegationPolicy   `json:"delegation,omitempty"`
	Restrictions     AgentRestrictions  `json:"restrictions,omitempty"`
	Communication    AgentCommunication `json:"communication,omitempty"`
	Workspace        AgentWorkspace     `json:"workspace,omitempty"`
	PromptSource     string             `json:"prompt_source,omitempty"`
	LegacySourcePath string             `json:"legacy_source_path,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type MaterializedDocRef struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

type EffectiveRuntime struct {
	InstanceID         string                  `json:"instance_id"`
	AgentID            string                  `json:"agent_id"`
	WorkspaceRoot      string                  `json:"workspace_root"`
	AgentWorkspaceRoot string                  `json:"agent_workspace_root,omitempty"`
	ControlPlaneDir    string                  `json:"control_plane_dir"`
	InstancesDir       string                  `json:"instances_dir"`
	InstanceDir        string                  `json:"instance_dir"`
	AgentsDir          string                  `json:"agents_dir"`
	AgentDir           string                  `json:"agent_dir"`
	DocsDir            string                  `json:"docs_dir"`
	Model              config.ModelConfig      `json:"model"`
	AllowedTools       []string                `json:"allowed_tools,omitempty"`
	PromptSourceMode   string                  `json:"prompt_source_mode"`
	PromptStackState   promptstack.PromptStack `json:"prompt_stack_state"`
	MaterializedDocs   []MaterializedDocRef    `json:"materialized_docs,omitempty"`
	Delegation         DelegationPolicy        `json:"delegation"`
	Messaging          MessagingPolicy         `json:"messaging"`
	RoleTemplates      []roles.RoleTemplate    `json:"role_templates,omitempty"`
	Skills             []string                `json:"skills,omitempty"`
	ChannelDefaults    map[string]ChannelRoute `json:"channel_defaults,omitempty"`
	Concurrency        RuntimeConfig           `json:"concurrency"`
	FeatureFlags       FeatureSet              `json:"feature_flags"`
	ThinkingMode       string                  `json:"thinking_mode,omitempty"`
	TimeoutMS          int                     `json:"timeout_ms,omitempty"`
	Enabled            bool                    `json:"enabled"`
	SourceMarker       string                  `json:"source_marker,omitempty"`
	LegacyConfig       config.Config           `json:"-"`
	InstanceManifest   InstanceManifest        `json:"-"`
	AgentManifest      AgentManifest           `json:"-"`
}

var canonicalAgentDocNames = []string{
	"SOUL.md",
	"RULES.md",
	"TOOLS.md",
	"SPECPLAN.md",
	"DEVPLAN.md",
	"HANDOFF.md",
}

func DefaultFeatureSet() FeatureSet {
	available := FeatureFlagState{Enabled: true, Visible: true}
	return FeatureSet{
		AgentContract:  available,
		PromptStack:    available,
		RoleTemplates:  available,
		Delegation:     available,
		Eval:           available,
		Instances:      available,
		AgentMessaging: available,
	}
}

func DefaultInstanceSettings() InstanceSettings {
	return InstanceSettings{
		ActiveInstanceID:                 DefaultInstanceID,
		AllowParallelInstances:           true,
		DefaultMaxConcurrentInstanceRuns: 8,
	}
}

func DefaultInstanceManifest(instanceID string) InstanceManifest {
	trimmed, _ := ValidateInstanceID(firstNonEmpty(instanceID, DefaultInstanceID))
	return InstanceManifest{
		InstanceID:  trimmed,
		DisplayName: trimmed,
		Enabled:     true,
		Workspace: WorkspaceConfig{
			Root:   filepathJoin("workspace", "instances", trimmed),
			Shared: true,
		},
		Runtime: RuntimeConfig{
			DefaultAgentID:    "default",
			EnabledAgentIDs:   []string{"default"},
			MaxConcurrentRuns: 8,
		},
		Prompting: PromptingConfig{
			SourceMode:      PromptSourcePromptStack,
			MaterializeDocs: true,
		},
		Delegation: DelegationPolicy{
			Enabled:            true,
			Mode:               "tool_gated",
			Threshold:          2,
			CooldownIterations: 15,
			MaxDepth:           3,
		},
		Messaging: MessagingPolicy{
			Enabled:                  true,
			AllowInterAgentMessaging: true,
			SharedInboxNamespace:     "instance",
			AllowCrossInstance:       false,
		},
		Skills:   SkillsConfig{Activated: []string{}},
		Roles:    RolesConfig{TemplateNames: []string{}},
		Channels: map[string]ChannelRoute{"dashboard": {DefaultAgentID: "default"}},
	}
}

func DefaultAgentManifest(agentID string) AgentManifest {
	trimmed, _ := ValidateAgentID(firstNonEmpty(agentID, "default"))
	return AgentManifest{
		AgentID:      trimmed,
		DisplayName:  trimmed,
		Enabled:      true,
		PromptSource: PromptSourcePromptStack,
	}
}

func ValidateFeatureSet(features FeatureSet) error {
	_ = features
	return nil
}

func ValidateInstanceManifest(manifest InstanceManifest) error {
	if _, err := ValidateInstanceID(manifest.InstanceID); err != nil {
		return fmt.Errorf("instance_id: %w", err)
	}
	if strings.TrimSpace(manifest.Workspace.Root) == "" {
		return fmt.Errorf("workspace.root is required")
	}
	if _, err := ValidateAgentID(firstNonEmpty(manifest.Runtime.DefaultAgentID, "default")); err != nil {
		return fmt.Errorf("runtime.default_agent_id: %w", err)
	}
	for _, id := range manifest.Runtime.EnabledAgentIDs {
		if _, err := ValidateAgentID(id); err != nil {
			return fmt.Errorf("runtime.enabled_agent_ids: %w", err)
		}
	}
	if manifest.Runtime.MaxConcurrentRuns < 0 {
		return fmt.Errorf("runtime.max_concurrent_runs must be >= 0")
	}
	if mode := strings.TrimSpace(manifest.Prompting.SourceMode); mode != "" && mode != PromptSourcePromptStack && mode != PromptSourceMigratedDocs && mode != PromptSourceLegacyDocs {
		return fmt.Errorf("prompting.source_mode %q is invalid", mode)
	}
	if manifest.Delegation.Threshold < 0 || manifest.Delegation.CooldownIterations < 0 || manifest.Delegation.MaxDepth < 0 {
		return fmt.Errorf("delegation numeric values must be >= 0")
	}
	for channel, route := range manifest.Channels {
		if strings.TrimSpace(channel) == "" {
			return fmt.Errorf("channels key is required")
		}
		if route.DefaultAgentID != "" {
			if _, err := ValidateAgentID(route.DefaultAgentID); err != nil {
				return fmt.Errorf("channels.%s.default_agent_id: %w", channel, err)
			}
		}
	}
	return ValidateFeatureSet(DefaultFeatureSet())
}

func ValidateAgentManifest(manifest AgentManifest) error {
	if _, err := ValidateAgentID(manifest.AgentID); err != nil {
		return fmt.Errorf("agent_id: %w", err)
	}
	if manifest.Restrictions.MaxToolIterations < 0 || manifest.Restrictions.TimeoutMS < 0 || manifest.Restrictions.MaxConcurrentRuns < 0 {
		return fmt.Errorf("agent restriction numeric values must be >= 0")
	}
	for _, id := range manifest.Communication.CanMessage {
		if _, err := ValidateAgentID(id); err != nil {
			return fmt.Errorf("communication.can_message: %w", err)
		}
	}
	for _, id := range manifest.Communication.CanReceiveFrom {
		if _, err := ValidateAgentID(id); err != nil {
			return fmt.Errorf("communication.can_receive_from: %w", err)
		}
	}
	if mode := strings.TrimSpace(manifest.PromptSource); mode != "" && mode != PromptSourcePromptStack && mode != PromptSourceMigratedDocs && mode != PromptSourceLegacyDocs {
		return fmt.Errorf("prompt_source %q is invalid", mode)
	}
	return nil
}

func CanonicalAgentDocNames() []string {
	out := make([]string, len(canonicalAgentDocNames))
	copy(out, canonicalAgentDocNames)
	return out
}

func NormalizeAgentDocName(name string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "SOUL", "SOUL.MD":
		return "SOUL.md", true
	case "RULES", "RULES.MD":
		return "RULES.md", true
	case "TOOLS", "TOOLS.MD":
		return "TOOLS.md", true
	case "SPECPLAN", "SPECPLAN.MD":
		return "SPECPLAN.md", true
	case "DEVPLAN", "DEVPLAN.MD":
		return "DEVPLAN.md", true
	case "HANDOFF", "HANDOFF.MD", "HEARTBEAT", "HEARTBEAT.MD":
		return "HANDOFF.md", true
	default:
		return "", false
	}
}

func SortedChannelNames(channels map[string]ChannelRoute) []string {
	out := make([]string, 0, len(channels))
	for name := range channels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func filepathJoin(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, "/")
}
