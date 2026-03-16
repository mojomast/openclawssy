package dashboard

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/instances"
	"openclawssy/internal/roles"
)

func (h *Handler) listProjectedInstances() ([]dashboardInstance, string, error) {
	manifests, err := instances.ListInstances(h.rootDir)
	if err != nil {
		return nil, "", err
	}
	activeInstanceID, err := instances.LoadActiveInstanceID(h.rootDir)
	if err != nil {
		return nil, "", err
	}
	projected := make([]dashboardInstance, 0, len(manifests))
	for _, manifest := range manifests {
		instance, err := h.projectDashboardInstance(manifest)
		if err != nil {
			return nil, "", err
		}
		projected = append(projected, instance)
	}
	sort.Slice(projected, func(i, j int) bool {
		if projected[i].UpdatedAt == projected[j].UpdatedAt {
			return projected[i].ID < projected[j].ID
		}
		return projected[i].UpdatedAt > projected[j].UpdatedAt
	})
	return projected, activeInstanceID, nil
}

func (h *Handler) loadProjectedInstance(instanceID string) (dashboardInstance, error) {
	normalized, err := normalizeInstanceID(instanceID)
	if err != nil {
		return dashboardInstance{}, err
	}
	manifest, err := instances.LoadInstanceManifest(h.rootDir, normalized)
	if err != nil {
		return dashboardInstance{}, err
	}
	return h.projectDashboardInstance(manifest)
}

func (h *Handler) projectDashboardInstance(manifest instances.InstanceManifest) (dashboardInstance, error) {
	cfg, err := h.projectDashboardConfig(manifest)
	if err != nil {
		return dashboardInstance{}, err
	}
	name := strings.TrimSpace(manifest.DisplayName)
	if name == "" {
		name = manifest.InstanceID
	}
	return dashboardInstance{
		ID:          manifest.InstanceID,
		Name:        name,
		Description: strings.TrimSpace(manifest.Description),
		Template:    "custom",
		CreatedAt:   manifest.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   manifest.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Config:      cfg,
	}, nil
}

func (h *Handler) projectDashboardConfig(manifest instances.InstanceManifest) (config.Config, error) {
	cfg, err := h.loadDashboardConfig()
	if err != nil {
		cfg = config.Default()
	}
	cfg.ApplyDefaults()
	cfg.Workspace.Root = manifest.Workspace.Root
	if manifest.Runtime.MaxConcurrentRuns > 0 {
		cfg.Engine.MaxConcurrentRuns = manifest.Runtime.MaxConcurrentRuns
	}
	cfg.Agents.AllowInterAgentMessaging = manifest.Messaging.AllowInterAgentMessaging
	cfg.Agents.EnabledAgentIDs = append([]string(nil), manifest.Runtime.EnabledAgentIDs...)
	cfg.Agents.DelegationMode = manifest.Delegation.Mode
	cfg.Agents.DelegationThreshold = manifest.Delegation.Threshold
	cfg.Agents.DelegationAgentID = manifest.Delegation.DelegationAgentID
	cfg.Agents.DelegationCooldownIter = manifest.Delegation.CooldownIterations
	roleTemplates, err := instances.LoadInstanceRoles(h.rootDir, manifest.InstanceID)
	if err == nil {
		cfg.Agents.CustomRoleTemplates = roleTemplates
	}
	channels, err := instances.LoadInstanceChannels(h.rootDir, manifest.InstanceID)
	if err != nil || len(channels) == 0 {
		channels = manifest.Channels
	}
	defaultAgentID := firstNonEmpty(channelDefaultAgentID(channels, "dashboard"), manifest.Runtime.DefaultAgentID)
	if defaultAgentID != "" {
		cfg.Chat.DefaultAgentID = defaultAgentID
	}
	if discordID := firstNonEmpty(channelDefaultAgentID(channels, "discord"), defaultAgentID); discordID != "" {
		cfg.Discord.DefaultAgentID = discordID
	}
	if telegramID := firstNonEmpty(channelDefaultAgentID(channels, "telegram"), defaultAgentID); telegramID != "" {
		cfg.Telegram.DefaultAgentID = telegramID
	}
	agents, err := instances.ListAgents(h.rootDir, manifest.InstanceID)
	if err != nil {
		return config.Config{}, err
	}
	profiles := make(map[string]config.AgentProfile, len(agents))
	for _, agent := range agents {
		enabled := agent.Enabled
		profiles[agent.AgentID] = config.AgentProfile{
			Enabled:         boolPtr(enabled),
			Model:           agent.Model,
			SelfImprovement: agent.Behavior.SelfImprovement,
		}
		if agent.AgentID == manifest.Runtime.DefaultAgentID {
			cfg.Model = mergeDashboardModel(cfg.Model, agent.Model)
		}
	}
	cfg.Agents.Profiles = profiles
	cfg.ApplyDefaults()
	return cfg, nil
}

func (h *Handler) saveProjectedInstance(instance dashboardInstance) error {
	manifest, agentManifests, roleTemplates, channels, err := h.instanceProjectionToCanonical(instance)
	if err != nil {
		return err
	}
	if err := instances.SaveInstanceManifest(h.rootDir, manifest); err != nil {
		return err
	}
	if err := instances.SaveInstanceRoles(h.rootDir, manifest.InstanceID, roleTemplates); err != nil {
		return err
	}
	if err := instances.SaveInstanceChannels(h.rootDir, manifest.InstanceID, channels); err != nil {
		return err
	}
	existingSkills, err := instances.LoadInstanceSkills(h.rootDir, manifest.InstanceID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := instances.SaveInstanceSkills(h.rootDir, manifest.InstanceID, existingSkills); err != nil {
		return err
	}
	existingAgents, err := instances.ListAgents(h.rootDir, manifest.InstanceID)
	if err != nil {
		return err
	}
	desiredAgentIDs := make(map[string]struct{}, len(agentManifests))
	for _, agentManifest := range agentManifests {
		desiredAgentIDs[agentManifest.AgentID] = struct{}{}
		if err := instances.SaveAgentManifest(h.rootDir, manifest.InstanceID, agentManifest); err != nil {
			return err
		}
	}
	for _, agentManifest := range existingAgents {
		if _, ok := desiredAgentIDs[agentManifest.AgentID]; ok {
			continue
		}
		if err := instances.DeleteAgent(h.rootDir, manifest.InstanceID, agentManifest.AgentID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) instanceProjectionToCanonical(instance dashboardInstance) (instances.InstanceManifest, []instances.AgentManifest, []roles.RoleTemplate, map[string]instances.ChannelRoute, error) {
	cfg, err := cloneDashboardConfig(instance.Config)
	if err != nil {
		return instances.InstanceManifest{}, nil, nil, nil, err
	}
	if err := cfg.Validate(); err != nil {
		return instances.InstanceManifest{}, nil, nil, nil, err
	}
	normalizedID, err := normalizeInstanceID(instance.ID)
	if err != nil {
		return instances.InstanceManifest{}, nil, nil, nil, err
	}
	manifest := instances.DefaultInstanceManifest(normalizedID)
	if existing, loadErr := instances.LoadInstanceManifest(h.rootDir, normalizedID); loadErr == nil {
		manifest = existing
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return instances.InstanceManifest{}, nil, nil, nil, loadErr
	}
	manifest.InstanceID = normalizedID
	manifest.DisplayName = firstNonEmpty(instance.Name, normalizedID)
	manifest.Description = strings.TrimSpace(instance.Description)
	if createdAt, ok := parseDashboardTimestamp(instance.CreatedAt); ok {
		manifest.CreatedAt = createdAt
	}
	manifest.Workspace.Root = cfg.Workspace.Root
	manifest.Runtime.DefaultAgentID = dashboardConfigDefaultAgentID(cfg, manifest.Runtime.DefaultAgentID)
	manifest.Runtime.EnabledAgentIDs = collectDashboardEnabledAgentIDs(cfg, manifest.Runtime.DefaultAgentID)
	manifest.Runtime.MaxConcurrentRuns = cfg.Engine.MaxConcurrentRuns
	manifest.Delegation.Enabled = true
	manifest.Delegation.Mode = strings.TrimSpace(cfg.Agents.DelegationMode)
	manifest.Delegation.Threshold = cfg.Agents.DelegationThreshold
	manifest.Delegation.CooldownIterations = cfg.Agents.DelegationCooldownIter
	manifest.Delegation.DelegationAgentID = strings.TrimSpace(cfg.Agents.DelegationAgentID)
	manifest.Messaging.Enabled = cfg.Agents.AllowInterAgentMessaging
	manifest.Messaging.AllowInterAgentMessaging = cfg.Agents.AllowInterAgentMessaging
	roleTemplates := append([]roles.RoleTemplate(nil), cfg.Agents.CustomRoleTemplates...)
	manifest.Roles.TemplateNames = dashboardRoleTemplateNames(roleTemplates)
	channels := dashboardChannelsFromConfig(cfg, manifest.Runtime.DefaultAgentID)
	manifest.Channels = channels
	agentManifests, err := h.agentManifestsFromDashboardConfig(normalizedID, cfg, manifest.Runtime.DefaultAgentID, manifest.Runtime.EnabledAgentIDs)
	if err != nil {
		return instances.InstanceManifest{}, nil, nil, nil, err
	}
	return manifest, agentManifests, roleTemplates, channels, nil
}

func (h *Handler) agentManifestsFromDashboardConfig(instanceID string, cfg config.Config, defaultAgentID string, enabledAgentIDs []string) ([]instances.AgentManifest, error) {
	desiredIDs := make(map[string]struct{})
	for _, id := range enabledAgentIDs {
		normalized, err := normalizeDashboardAgentID(id)
		if err != nil {
			return nil, err
		}
		desiredIDs[normalized] = struct{}{}
	}
	for id := range cfg.Agents.Profiles {
		normalized, err := normalizeDashboardAgentID(id)
		if err != nil {
			return nil, err
		}
		desiredIDs[normalized] = struct{}{}
	}
	if normalizedDefault, err := normalizeDashboardAgentID(defaultAgentID); err == nil {
		desiredIDs[normalizedDefault] = struct{}{}
	}
	if len(desiredIDs) == 0 {
		desiredIDs["default"] = struct{}{}
	}
	agentIDs := make([]string, 0, len(desiredIDs))
	for id := range desiredIDs {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)
	out := make([]instances.AgentManifest, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		manifest := instances.DefaultAgentManifest(agentID)
		if existing, err := instances.LoadAgentManifest(h.rootDir, instanceID, agentID); err == nil {
			manifest = existing
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		manifest.AgentID = agentID
		manifest.DisplayName = firstNonEmpty(manifest.DisplayName, agentID)
		manifest.Restrictions.AllowedTools = append([]string(nil), cfg.Agents.SubAgentDefaults.AllowedTools...)
		manifest.Restrictions.MaxToolIterations = cfg.Agents.SubAgentDefaults.MaxToolIterations
		manifest.Restrictions.TimeoutMS = cfg.Agents.SubAgentDefaults.TimeoutMS
		manifest.Behavior.DefaultThinkingMode = firstNonEmpty(manifest.Behavior.DefaultThinkingMode, cfg.Agents.SubAgentDefaults.ThinkingMode)
		if profile, ok := cfg.Agents.Profiles[agentID]; ok {
			if profile.Enabled != nil {
				manifest.Enabled = *profile.Enabled
			} else {
				manifest.Enabled = containsString(enabledAgentIDs, agentID)
			}
			manifest.Model = profile.Model
			manifest.Behavior.SelfImprovement = profile.SelfImprovement
		} else {
			manifest.Enabled = containsString(enabledAgentIDs, agentID)
		}
		out = append(out, manifest)
	}
	return out, nil
}

func (h *Handler) projectedInstanceExists(instanceID string) (bool, error) {
	normalized, err := normalizeInstanceID(instanceID)
	if err != nil {
		return false, err
	}
	_, err = instances.LoadInstanceManifest(h.rootDir, normalized)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (h *Handler) hasCanonicalActiveInstanceSelection() bool {
	_, err := os.Stat(instances.ActiveInstancePath(h.rootDir))
	return err == nil
}

func channelDefaultAgentID(channels map[string]instances.ChannelRoute, channel string) string {
	return strings.TrimSpace(channels[strings.TrimSpace(channel)].DefaultAgentID)
}

func dashboardConfigDefaultAgentID(cfg config.Config, fallback string) string {
	defaultAgentID, err := normalizeDashboardAgentID(firstNonEmpty(cfg.Chat.DefaultAgentID, cfg.Discord.DefaultAgentID, cfg.Telegram.DefaultAgentID, fallback))
	if err != nil {
		return fallback
	}
	return defaultAgentID
}

func collectDashboardEnabledAgentIDs(cfg config.Config, fallback string) []string {
	set := map[string]struct{}{}
	for _, agentID := range cfg.Agents.EnabledAgentIDs {
		if normalized, err := normalizeDashboardAgentID(agentID); err == nil {
			set[normalized] = struct{}{}
		}
	}
	for agentID := range cfg.Agents.Profiles {
		if normalized, err := normalizeDashboardAgentID(agentID); err == nil {
			set[normalized] = struct{}{}
		}
	}
	for _, agentID := range []string{cfg.Chat.DefaultAgentID, cfg.Discord.DefaultAgentID, cfg.Telegram.DefaultAgentID, fallback} {
		if normalized, err := normalizeDashboardAgentID(agentID); err == nil {
			set[normalized] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for agentID := range set {
		out = append(out, agentID)
	}
	sort.Strings(out)
	return out
}

func dashboardChannelsFromConfig(cfg config.Config, fallback string) map[string]instances.ChannelRoute {
	routes := map[string]instances.ChannelRoute{}
	if dashboardID := firstNonEmpty(strings.TrimSpace(cfg.Chat.DefaultAgentID), fallback); dashboardID != "" {
		routes["dashboard"] = instances.ChannelRoute{DefaultAgentID: dashboardID}
	}
	if discordID := firstNonEmpty(strings.TrimSpace(cfg.Discord.DefaultAgentID), fallback); discordID != "" {
		routes["discord"] = instances.ChannelRoute{DefaultAgentID: discordID}
	}
	if telegramID := firstNonEmpty(strings.TrimSpace(cfg.Telegram.DefaultAgentID), fallback); telegramID != "" {
		routes["telegram"] = instances.ChannelRoute{DefaultAgentID: telegramID}
	}
	return routes
}

func mergeDashboardModel(base, override config.ModelConfig) config.ModelConfig {
	merged := base
	if strings.TrimSpace(override.Provider) != "" {
		merged.Provider = strings.TrimSpace(override.Provider)
	}
	if strings.TrimSpace(override.Name) != "" {
		merged.Name = strings.TrimSpace(override.Name)
	}
	if override.Temperature != 0 {
		merged.Temperature = override.Temperature
	}
	if override.MaxTokens > 0 {
		merged.MaxTokens = override.MaxTokens
	}
	if override.TimeoutMS > 0 {
		merged.TimeoutMS = override.TimeoutMS
	}
	return merged
}

func parseDashboardTimestamp(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func dashboardRoleTemplateNames(templates []roles.RoleTemplate) []string {
	names := make([]string, 0, len(templates))
	for _, template := range templates {
		name := strings.TrimSpace(template.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
