package contract

import (
	"fmt"
	"sort"
	"strings"

	"openclawssy/internal/agent"
	"openclawssy/internal/agentdocs"
	"openclawssy/internal/config"
)

type ResolveOptions struct {
	SubAgent bool
}

type Resolver struct {
	cfg config.Config
}

func NewResolver(cfg config.Config) *Resolver {
	return &Resolver{cfg: cfg}
}

func Resolve(cfg config.Config, agentID string, opts *ResolveOptions) (AgentContract, error) {
	return NewResolver(cfg).Resolve(agentID, opts)
}

func (r *Resolver) Resolve(agentID string, opts *ResolveOptions) (AgentContract, error) {
	if r == nil {
		return AgentContract{}, fmt.Errorf("resolve contract: resolver is nil")
	}

	resolvedAgentID := strings.TrimSpace(agentID)
	if resolvedAgentID == "" {
		return AgentContract{}, fmt.Errorf("resolve contract: agent id cannot be empty")
	}

	cfg := r.cfg
	cfg.ApplyDefaults()

	contract := AgentContract{
		Identity: Identity{
			AgentID:     resolvedAgentID,
			DisplayName: resolvedAgentID,
		},
		Mission: Mission{
			Description: fmt.Sprintf("Resolved runtime contract for agent %s", resolvedAgentID),
		},
		SystemPrompt: SystemPrompt{
			Content: buildAgentPrompt(resolvedAgentID),
			Source:  "agentdocs",
		},
		ToolPolicy: ToolPolicy{
			AllowedTools: cloneStrings(cfg.Agents.SubAgentDefaults.AllowedTools),
			DefaultDeny:  true,
		},
		DelegationPolicy: DelegationPolicy{
			Mode:         strings.TrimSpace(cfg.Agents.DelegationMode),
			Threshold:    cfg.Agents.DelegationThreshold,
			Cooldown:     cfg.Agents.DelegationCooldownIter,
			AutoDelegate: cfg.Agents.AutoDelegate,
			AgentID:      strings.TrimSpace(cfg.Agents.DelegationAgentID),
			MaxDepth:     cfg.Agents.SubAgentDefaults.MaxToolIterations,
		},
		MemoryPolicy: MemoryPolicy{
			Enabled:         cfg.Memory.Enabled,
			MaxWorkingItems: cfg.Memory.MaxWorkingItems,
			MaxPromptTokens: cfg.Memory.MaxPromptTokens,
			AutoCheckpoint:  cfg.Memory.AutoCheckpoint,
			Proactive:       cfg.Memory.ProactiveEnabled,
			Embeddings:      cfg.Memory.EmbeddingsEnabled,
		},
		ModelPolicy: ModelPolicy{
			Provider:    strings.TrimSpace(cfg.Model.Provider),
			Model:       strings.TrimSpace(cfg.Model.Name),
			Temperature: cfg.Model.Temperature,
			MaxTokens:   cfg.Model.MaxTokens,
			TimeoutMS:   cfg.Model.TimeoutMS,
		},
		SandboxPolicy: SandboxPolicy{
			Active:   cfg.Sandbox.Active,
			Provider: strings.TrimSpace(cfg.Sandbox.Provider),
			Docker:   toContractDockerConfig(cfg.Sandbox.Docker),
		},
		ObservabilityPolicy: ObservabilityPolicy{
			AuditEnabled: true,
			TraceEnabled: true,
			ThinkingMode: config.NormalizeThinkingMode(cfg.Output.ThinkingMode),
		},
		Inheritance: Inheritance{Source: initializeInheritanceSources()},
	}

	if contract.DelegationPolicy.AgentID == "" {
		contract.DelegationPolicy.AgentID = resolvedAgentID
	}

	if profile, ok := cfg.Agents.Profiles[resolvedAgentID]; ok {
		if cfg.Agents.AllowAgentModelOverrides {
			applyProfileModelOverrides(&contract, profile, contract.Inheritance.Source)
		}
		if profile.SelfImprovement {
			contract.Mission.Goals = appendUnique(contract.Mission.Goals, "self-improvement")
			setInheritanceSource(contract.Inheritance.Source, "mission.goals", InheritanceSourceAgentProfile)
		}
	}

	if opts != nil && opts.SubAgent {
		restrictions, sources := resolveSubAgentRestrictionsWithSources(cfg.Agents.SubAgentDefaults, cfg.Agents.SubAgentOverrides[resolvedAgentID])

		if len(restrictions.AllowedTools) > 0 {
			contract.ToolPolicy.AllowedTools = cloneStrings(restrictions.AllowedTools)
			setInheritanceSource(contract.Inheritance.Source, "tool_policy.allowed_tools", sources.allowedTools)
		}
		if mode := strings.TrimSpace(restrictions.DelegationMode); mode != "" {
			contract.DelegationPolicy.Mode = mode
			setInheritanceSource(contract.Inheritance.Source, "delegation_policy.mode", sources.delegationMode)
		}
		if restrictions.MaxToolIterations > 0 {
			contract.DelegationPolicy.MaxDepth = restrictions.MaxToolIterations
			setInheritanceSource(contract.Inheritance.Source, "delegation_policy.max_depth", sources.maxToolIterations)
		}
		if restrictions.TimeoutMS > 0 {
			contract.ModelPolicy.TimeoutMS = restrictions.TimeoutMS
			setInheritanceSource(contract.Inheritance.Source, "model_policy.timeout_ms", sources.timeoutMS)
		}
		if thinkingMode := strings.TrimSpace(restrictions.ThinkingMode); thinkingMode != "" {
			contract.ObservabilityPolicy.ThinkingMode = config.NormalizeThinkingMode(thinkingMode)
			setInheritanceSource(contract.Inheritance.Source, "observability_policy.thinking_mode", sources.thinkingMode)
		}
	}

	if err := contract.Validate(); err != nil {
		return AgentContract{}, fmt.Errorf("resolve contract for %s: %w", resolvedAgentID, err)
	}

	return contract, nil
}

func applyProfileModelOverrides(contract *AgentContract, profile config.AgentProfile, sourceMap map[string]string) {
	provider := strings.TrimSpace(profile.Model.Provider)
	if provider != "" {
		contract.ModelPolicy.Provider = provider
		setInheritanceSource(sourceMap, "model_policy.provider", InheritanceSourceAgentProfile)
	}

	name := strings.TrimSpace(profile.Model.Name)
	if name != "" {
		contract.ModelPolicy.Model = name
		setInheritanceSource(sourceMap, "model_policy.model", InheritanceSourceAgentProfile)
	}

	if profile.Model.Temperature != 0 {
		contract.ModelPolicy.Temperature = profile.Model.Temperature
		setInheritanceSource(sourceMap, "model_policy.temperature", InheritanceSourceAgentProfile)
	}

	if profile.Model.MaxTokens > 0 {
		contract.ModelPolicy.MaxTokens = profile.Model.MaxTokens
		setInheritanceSource(sourceMap, "model_policy.max_tokens", InheritanceSourceAgentProfile)
	}

	if profile.Model.TimeoutMS > 0 {
		contract.ModelPolicy.TimeoutMS = profile.Model.TimeoutMS
		setInheritanceSource(sourceMap, "model_policy.timeout_ms", InheritanceSourceAgentProfile)
	}
}

type subAgentRestrictionSources struct {
	allowedTools      string
	maxToolIterations string
	timeoutMS         string
	thinkingMode      string
	delegationMode    string
}

func resolveSubAgentRestrictionsWithSources(defaults, override config.SubAgentRestrictions) (config.SubAgentRestrictions, subAgentRestrictionSources) {
	resolved := defaults
	sources := subAgentRestrictionSources{
		allowedTools:      InheritanceSourceGlobal,
		maxToolIterations: InheritanceSourceGlobal,
		timeoutMS:         InheritanceSourceGlobal,
		thinkingMode:      InheritanceSourceGlobal,
		delegationMode:    InheritanceSourceGlobal,
	}

	if len(override.AllowedTools) > 0 {
		resolved.AllowedTools = override.AllowedTools
		sources.allowedTools = InheritanceSourceSubagentOverride
	}
	if override.MaxToolIterations > 0 {
		resolved.MaxToolIterations = override.MaxToolIterations
		sources.maxToolIterations = InheritanceSourceSubagentOverride
	}
	if override.TimeoutMS > 0 {
		resolved.TimeoutMS = override.TimeoutMS
		sources.timeoutMS = InheritanceSourceSubagentOverride
	}
	if strings.TrimSpace(override.ThinkingMode) != "" {
		resolved.ThinkingMode = strings.TrimSpace(override.ThinkingMode)
		sources.thinkingMode = InheritanceSourceSubagentOverride
	}
	if strings.TrimSpace(override.DelegationMode) != "" {
		resolved.DelegationMode = strings.TrimSpace(override.DelegationMode)
		sources.delegationMode = InheritanceSourceSubagentOverride
	}

	return resolved, sources
}

func buildAgentPrompt(agentID string) string {
	scaffoldFiles := agentdocs.ScaffoldFilesForAgent(agentID)
	docs := make([]agent.ArtifactDoc, 0, len(scaffoldFiles))

	for _, name := range []string{"SOUL.md", "RULES.md", "TOOLS.md", "SPECPLAN.md", "DEVPLAN.md", "HANDOFF.md"} {
		content, ok := scaffoldFiles[name]
		if !ok {
			continue
		}
		docs = append(docs, agent.ArtifactDoc{Name: name, Content: content})
		delete(scaffoldFiles, name)
	}

	extraNames := make([]string, 0, len(scaffoldFiles))
	for name := range scaffoldFiles {
		extraNames = append(extraNames, name)
	}
	sort.Strings(extraNames)
	for _, name := range extraNames {
		docs = append(docs, agent.ArtifactDoc{Name: name, Content: scaffoldFiles[name]})
	}

	return agent.AssemblePrompt(docs, 0)
}

func setInheritanceSource(sourceMap map[string]string, key, source string) {
	if sourceMap == nil || strings.TrimSpace(key) == "" {
		return
	}

	if idx := strings.IndexByte(key, '.'); idx > 0 {
		sourceMap[key] = source
		section := key[:idx]
		if current, ok := sourceMap[section]; !ok || sourcePrecedence(source) >= sourcePrecedence(current) {
			sourceMap[section] = source
		}
		return
	}

	sourceMap[key] = source
}

func sourcePrecedence(source string) int {
	switch strings.TrimSpace(source) {
	case InheritanceSourceSubagentOverride:
		return 3
	case InheritanceSourceAgentProfile:
		return 2
	default:
		return 1
	}
}

func initializeInheritanceSources() map[string]string {
	keys := []string{
		"identity",
		"identity.agent_id",
		"identity.display_name",
		"mission",
		"mission.description",
		"mission.goals",
		"system_prompt",
		"system_prompt.content",
		"system_prompt.source",
		"tool_policy",
		"tool_policy.allowed_tools",
		"tool_policy.denied_tools",
		"tool_policy.default_deny",
		"delegation_policy",
		"delegation_policy.mode",
		"delegation_policy.threshold",
		"delegation_policy.cooldown",
		"delegation_policy.auto_delegate",
		"delegation_policy.agent_id",
		"delegation_policy.max_depth",
		"memory_policy",
		"memory_policy.enabled",
		"memory_policy.max_working_items",
		"memory_policy.max_prompt_tokens",
		"memory_policy.auto_checkpoint",
		"memory_policy.proactive",
		"memory_policy.embeddings",
		"model_policy",
		"model_policy.provider",
		"model_policy.model",
		"model_policy.temperature",
		"model_policy.max_tokens",
		"model_policy.timeout_ms",
		"sandbox_policy",
		"sandbox_policy.active",
		"sandbox_policy.provider",
		"sandbox_policy.docker",
		"sandbox_policy.docker.image",
		"sandbox_policy.docker.host",
		"sandbox_policy.docker.network_enabled",
		"sandbox_policy.docker.cpu_limit",
		"sandbox_policy.docker.memory_limit_mb",
		"sandbox_policy.docker.hardened",
		"sandbox_policy.docker.require_dedicated_daemon",
		"sandbox_policy.docker.allowed_images",
		"sandbox_policy.docker.pids_limit",
		"sandbox_policy.docker.extra_env",
		"sandbox_policy.docker.mounts",
		"sandbox_policy.docker.pull_policy",
		"observability_policy",
		"observability_policy.audit_enabled",
		"observability_policy.trace_enabled",
		"observability_policy.thinking_mode",
	}

	sources := make(map[string]string, len(keys))
	for _, key := range keys {
		sources[key] = InheritanceSourceGlobal
	}
	return sources
}

func toContractDockerConfig(in config.DockerSandboxConfig) SandboxDockerConfig {
	mounts := make([]DockerMount, 0, len(in.Mounts))
	for _, mount := range in.Mounts {
		mounts = append(mounts, DockerMount{
			HostPath:      mount.HostPath,
			ContainerPath: mount.ContainerPath,
			ReadOnly:      mount.ReadOnly,
		})
	}

	return SandboxDockerConfig{
		Image:                  in.Image,
		Host:                   in.Host,
		NetworkEnabled:         in.NetworkEnabled,
		CPULimit:               in.CPULimit,
		MemoryLimitMB:          in.MemoryLimitMB,
		Hardened:               in.Hardened,
		RequireDedicatedDaemon: in.RequireDedicatedDaemon,
		AllowedImages:          cloneStrings(in.AllowedImages),
		PidsLimit:              in.PidsLimit,
		ExtraEnv:               cloneStrings(in.ExtraEnv),
		Mounts:                 mounts,
		PullPolicy:             in.PullPolicy,
	}
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func appendUnique(values []string, next string) []string {
	trimmed := strings.TrimSpace(next)
	if trimmed == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), trimmed) {
			return values
		}
	}
	return append(values, trimmed)
}
