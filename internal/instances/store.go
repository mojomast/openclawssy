package instances

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/agentdocs"
	"openclawssy/internal/config"
	"openclawssy/internal/fsutil"
	"openclawssy/internal/promptstack"
	"openclawssy/internal/roles"
)

func LoadInstanceManifest(rootDir, instanceID string) (InstanceManifest, error) {
	path, err := InstanceManifestPath(rootDir, instanceID)
	if err != nil {
		return InstanceManifest{}, err
	}
	manifest := DefaultInstanceManifest(instanceID)
	if err := loadJSONFile(path, &manifest); err != nil {
		return InstanceManifest{}, err
	}
	if err := ValidateInstanceManifest(manifest); err != nil {
		return InstanceManifest{}, err
	}
	return manifest, nil
}

func SaveInstanceManifest(rootDir string, manifest InstanceManifest) error {
	instanceID, err := ValidateInstanceID(firstNonEmpty(manifest.InstanceID, DefaultInstanceID))
	if err != nil {
		return err
	}
	defaults := DefaultInstanceManifest(instanceID)
	manifest.InstanceID = instanceID
	manifest.DisplayName = firstNonEmpty(manifest.DisplayName, defaults.DisplayName)
	if strings.TrimSpace(manifest.Workspace.Root) == "" {
		manifest.Workspace = defaults.Workspace
	}
	if strings.TrimSpace(manifest.Runtime.DefaultAgentID) == "" {
		manifest.Runtime.DefaultAgentID = defaults.Runtime.DefaultAgentID
	}
	if len(manifest.Runtime.EnabledAgentIDs) == 0 {
		manifest.Runtime.EnabledAgentIDs = append([]string(nil), defaults.Runtime.EnabledAgentIDs...)
	}
	if manifest.Runtime.MaxConcurrentRuns == 0 {
		manifest.Runtime.MaxConcurrentRuns = defaults.Runtime.MaxConcurrentRuns
	}
	if strings.TrimSpace(manifest.Prompting.SourceMode) == "" {
		manifest.Prompting = defaults.Prompting
	}
	if manifest.Messaging.SharedInboxNamespace == "" {
		manifest.Messaging.SharedInboxNamespace = defaults.Messaging.SharedInboxNamespace
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = time.Now().UTC()
	}
	manifest.UpdatedAt = time.Now().UTC()
	if err := ValidateInstanceManifest(manifest); err != nil {
		return err
	}
	path, err := InstanceManifestPath(rootDir, instanceID)
	if err != nil {
		return err
	}
	return writeJSONFile(path, manifest)
}

func ListInstances(rootDir string) ([]InstanceManifest, error) {
	entries, err := os.ReadDir(InstancesDir(rootDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []InstanceManifest{}, nil
		}
		return nil, err
	}
	out := make([]InstanceManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := LoadInstanceManifest(rootDir, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].InstanceID < out[j].InstanceID
	})
	return out, nil
}

func DeleteInstance(rootDir, instanceID string) error {
	instanceDir, err := InstanceDir(rootDir, instanceID)
	if err != nil {
		return err
	}
	return os.RemoveAll(instanceDir)
}

func LoadAgentManifest(rootDir, instanceID, agentID string) (AgentManifest, error) {
	path, err := AgentManifestPath(rootDir, instanceID, agentID)
	if err != nil {
		return AgentManifest{}, err
	}
	manifest := DefaultAgentManifest(agentID)
	if err := loadJSONFile(path, &manifest); err != nil {
		return AgentManifest{}, err
	}
	if err := ValidateAgentManifest(manifest); err != nil {
		return AgentManifest{}, err
	}
	return manifest, nil
}

func SaveAgentManifest(rootDir, instanceID string, manifest AgentManifest) error {
	if _, err := LoadInstanceManifest(rootDir, instanceID); err != nil {
		return err
	}
	agentID, err := ValidateAgentID(firstNonEmpty(manifest.AgentID, "default"))
	if err != nil {
		return err
	}
	defaults := DefaultAgentManifest(agentID)
	manifest.AgentID = agentID
	manifest.DisplayName = firstNonEmpty(manifest.DisplayName, defaults.DisplayName)
	if strings.TrimSpace(manifest.Behavior.DefaultThinkingMode) == "" {
		manifest.Behavior.DefaultThinkingMode = defaults.Behavior.DefaultThinkingMode
	}
	if strings.TrimSpace(manifest.PromptSource) == "" {
		manifest.PromptSource = PromptSourcePromptStack
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = time.Now().UTC()
	}
	manifest.UpdatedAt = time.Now().UTC()
	if err := ValidateAgentManifest(manifest); err != nil {
		return err
	}
	path, err := AgentManifestPath(rootDir, instanceID, agentID)
	if err != nil {
		return err
	}
	return writeJSONFile(path, manifest)
}

func ListAgents(rootDir, instanceID string) ([]AgentManifest, error) {
	if _, err := ValidateInstanceID(instanceID); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(mustAgentsDir(rootDir, instanceID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []AgentManifest{}, nil
		}
		return nil, err
	}
	out := make([]AgentManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := LoadAgentManifest(rootDir, instanceID, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AgentID < out[j].AgentID
	})
	return out, nil
}

func DeleteAgent(rootDir, instanceID, agentID string) error {
	agentDir, err := AgentDir(rootDir, instanceID, agentID)
	if err != nil {
		return err
	}
	return os.RemoveAll(agentDir)
}

func LoadInstanceRoles(rootDir, instanceID string) ([]roles.RoleTemplate, error) {
	path, err := InstanceRolesPath(rootDir, instanceID)
	if err != nil {
		return nil, err
	}
	var templates []roles.RoleTemplate
	if err := loadJSONFile(path, &templates); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []roles.RoleTemplate{}, nil
		}
		return nil, err
	}
	return templates, nil
}

func SaveInstanceRoles(rootDir, instanceID string, templates []roles.RoleTemplate) error {
	if _, err := LoadInstanceManifest(rootDir, instanceID); err != nil {
		return err
	}
	path, err := InstanceRolesPath(rootDir, instanceID)
	if err != nil {
		return err
	}
	return writeJSONFile(path, templates)
}

func LoadInstanceSkills(rootDir, instanceID string) (SkillsConfig, error) {
	path, err := InstanceSkillsPath(rootDir, instanceID)
	if err != nil {
		return SkillsConfig{}, err
	}
	var skills SkillsConfig
	if err := loadJSONFile(path, &skills); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SkillsConfig{Activated: []string{}}, nil
		}
		return SkillsConfig{}, err
	}
	if skills.Activated == nil {
		skills.Activated = []string{}
	}
	return skills, nil
}

func SaveInstanceSkills(rootDir, instanceID string, skills SkillsConfig) error {
	if _, err := LoadInstanceManifest(rootDir, instanceID); err != nil {
		return err
	}
	if skills.Activated == nil {
		skills.Activated = []string{}
	}
	path, err := InstanceSkillsPath(rootDir, instanceID)
	if err != nil {
		return err
	}
	return writeJSONFile(path, skills)
}

func LoadInstanceChannels(rootDir, instanceID string) (map[string]ChannelRoute, error) {
	path, err := InstanceChannelsPath(rootDir, instanceID)
	if err != nil {
		return nil, err
	}
	channels := map[string]ChannelRoute{}
	if err := loadJSONFile(path, &channels); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]ChannelRoute{}, nil
		}
		return nil, err
	}
	return channels, nil
}

func SaveInstanceChannels(rootDir, instanceID string, channels map[string]ChannelRoute) error {
	if _, err := LoadInstanceManifest(rootDir, instanceID); err != nil {
		return err
	}
	if channels == nil {
		channels = map[string]ChannelRoute{}
	}
	path, err := InstanceChannelsPath(rootDir, instanceID)
	if err != nil {
		return err
	}
	return writeJSONFile(path, channels)
}

func ActiveInstancePath(rootDir string) string {
	return filepath.Join(InstancesDir(rootDir), "active.json")
}

func LoadActiveInstanceID(rootDir string) (string, error) {
	raw, err := os.ReadFile(ActiveInstancePath(rootDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultInstanceID, nil
		}
		return "", err
	}
	var payload struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.InstanceID) == "" {
		return DefaultInstanceID, nil
	}
	return ValidateInstanceID(payload.InstanceID)
}

func LoadActiveInstanceManifest(rootDir string) (InstanceManifest, error) {
	instanceID, err := LoadActiveInstanceID(rootDir)
	if err != nil {
		return InstanceManifest{}, err
	}
	return LoadInstanceManifest(rootDir, instanceID)
}

func SaveActiveInstanceID(rootDir, instanceID string) error {
	validated, err := ValidateInstanceID(instanceID)
	if err != nil {
		return err
	}
	return writeJSONFile(ActiveInstancePath(rootDir), map[string]string{"instance_id": validated})
}

func ActivateInstance(rootDir, instanceID string) (InstanceManifest, error) {
	manifest, err := LoadInstanceManifest(rootDir, instanceID)
	if err != nil {
		return InstanceManifest{}, err
	}
	if err := SaveActiveInstanceID(rootDir, manifest.InstanceID); err != nil {
		return InstanceManifest{}, err
	}
	return manifest, nil
}

func BootstrapDefaultInstance(rootDir string) (InstanceManifest, error) {
	manifestPath, err := InstanceManifestPath(rootDir, DefaultInstanceID)
	if err != nil {
		return InstanceManifest{}, err
	}
	var manifest InstanceManifest
	if _, err := os.Stat(manifestPath); err == nil {
		manifest, err = LoadInstanceManifest(rootDir, DefaultInstanceID)
		if err != nil {
			return InstanceManifest{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstanceManifest{}, err
	} else {
		manifest = bootstrapInstanceManifestFromLegacyConfig(rootDir)
		if err := SaveInstanceManifest(rootDir, manifest); err != nil {
			return InstanceManifest{}, err
		}
	}
	legacyAgents, err := listLegacyAgentIDs(rootDir)
	if err != nil {
		return InstanceManifest{}, err
	}
	if len(legacyAgents) == 0 {
		legacyAgents = []string{"default"}
	}
	for _, agentID := range legacyAgents {
		if err := ensureBootstrappedAgent(rootDir, DefaultInstanceID, agentID); err != nil {
			return InstanceManifest{}, err
		}
	}
	if err := SaveInstanceRoles(rootDir, DefaultInstanceID, loadLegacyRoleTemplates(rootDir)); err != nil {
		return InstanceManifest{}, err
	}
	if err := SaveInstanceSkills(rootDir, DefaultInstanceID, loadLegacySkills(rootDir, legacyAgents)); err != nil {
		return InstanceManifest{}, err
	}
	if err := SaveInstanceChannels(rootDir, DefaultInstanceID, manifest.Channels); err != nil {
		return InstanceManifest{}, err
	}
	if err := SaveActiveInstanceID(rootDir, DefaultInstanceID); err != nil {
		return InstanceManifest{}, err
	}
	return LoadInstanceManifest(rootDir, DefaultInstanceID)
}

func SeedLegacyAgentDocs(rootDir, instanceID, agentID string) ([]string, error) {
	if _, err := ValidateInstanceID(instanceID); err != nil {
		return nil, err
	}
	agentID, err := ValidateAgentID(agentID)
	if err != nil {
		return nil, err
	}
	docsDir, err := AgentDocsDir(rootDir, instanceID, agentID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return nil, err
	}
	legacyDir, err := LegacyAgentDir(rootDir, agentID)
	if err != nil {
		return nil, err
	}
	legacyScaffold := agentdocs.ScaffoldFilesForAgent(agentID)
	seeded := make([]string, 0, len(canonicalAgentDocNames)+1)
	for _, name := range canonicalAgentDocNames {
		dstPath, err := AgentDocPath(rootDir, instanceID, agentID, name)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(dstPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		data, err := readLegacyDocOrScaffold(legacyDir, name, legacyScaffold)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			continue
		}
		if err := fsutil.WriteFileAtomic(dstPath, data, 0o600); err != nil {
			return nil, err
		}
		seeded = append(seeded, name)
	}
	if err := copyLegacyIdentityIfPresent(rootDir, instanceID, agentID, legacyDir); err != nil {
		return nil, err
	}
	sort.Strings(seeded)
	return seeded, nil
}

func ResolveEffectiveRuntime(rootDir, instanceID, agentID string) (*EffectiveRuntime, error) {
	if strings.TrimSpace(instanceID) == "" {
		activeID, err := LoadActiveInstanceID(rootDir)
		if err != nil {
			return nil, err
		}
		instanceID = activeID
	}
	instanceManifest, err := LoadInstanceManifest(rootDir, instanceID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(agentID) == "" {
		agentID = instanceManifest.Runtime.DefaultAgentID
	}
	agentManifest, err := LoadAgentManifest(rootDir, instanceID, agentID)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadOrDefault(filepath.Join(ControlPlaneDir(rootDir), "config.json"))
	if err != nil {
		return nil, err
	}
	instanceDir, _ := InstanceDir(rootDir, instanceID)
	agentsDir, _ := AgentsDir(rootDir, instanceID)
	agentDir, _ := AgentDir(rootDir, instanceID, agentID)
	docsDir, _ := AgentDocsDir(rootDir, instanceID, agentID)
	workspaceRoot := strings.TrimSpace(instanceManifest.Workspace.Root)
	if !filepath.IsAbs(workspaceRoot) {
		workspaceRoot = filepath.Join(rootDir, workspaceRoot)
	}
	agentWorkspaceRoot := workspaceRoot
	if strings.TrimSpace(agentManifest.Workspace.OverlayRoot) != "" {
		agentWorkspaceRoot = filepath.Join(workspaceRoot, agentManifest.Workspace.OverlayRoot)
	}
	model := cfg.Model
	if strings.TrimSpace(agentManifest.Model.Provider) != "" {
		model.Provider = strings.TrimSpace(agentManifest.Model.Provider)
	}
	if strings.TrimSpace(agentManifest.Model.Name) != "" {
		model.Name = strings.TrimSpace(agentManifest.Model.Name)
	}
	if agentManifest.Model.Temperature != 0 {
		model.Temperature = agentManifest.Model.Temperature
	}
	if agentManifest.Model.MaxTokens > 0 {
		model.MaxTokens = agentManifest.Model.MaxTokens
	}
	if agentManifest.Model.TimeoutMS > 0 {
		model.TimeoutMS = agentManifest.Model.TimeoutMS
	}
	stackStore, err := promptstack.NewVersionStore(ControlPlaneDir(rootDir), instanceID)
	if err != nil {
		return nil, err
	}
	if err := ensurePromptStackInitialized(rootDir, instanceID, agentID, stackStore); err != nil {
		return nil, err
	}
	stack, err := stackStore.GetCurrent(agentID)
	if err != nil {
		return nil, err
	}
	materializedDocs := make([]MaterializedDocRef, 0, len(CanonicalAgentDocNames()))
	for _, name := range CanonicalAgentDocNames() {
		path, pathErr := AgentDocPath(rootDir, instanceID, agentID, name)
		if pathErr != nil {
			continue
		}
		if _, statErr := os.Stat(path); statErr == nil {
			materializedDocs = append(materializedDocs, MaterializedDocRef{Name: name, Path: path})
		}
	}
	roleTemplates, _ := LoadInstanceRoles(rootDir, instanceID)
	skills, _ := LoadInstanceSkills(rootDir, instanceID)
	channels, _ := LoadInstanceChannels(rootDir, instanceID)
	allowedTools := append([]string(nil), agentManifest.Restrictions.AllowedTools...)
	if len(allowedTools) == 0 {
		allowedTools = append([]string(nil), cfg.Agents.SubAgentDefaults.AllowedTools...)
	}
	timeoutMS := agentManifest.Restrictions.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = model.TimeoutMS
	}
	resolved := &EffectiveRuntime{
		InstanceID:         instanceID,
		AgentID:            agentID,
		WorkspaceRoot:      filepath.Clean(workspaceRoot),
		AgentWorkspaceRoot: filepath.Clean(agentWorkspaceRoot),
		ControlPlaneDir:    ControlPlaneDir(rootDir),
		InstancesDir:       InstancesDir(rootDir),
		InstanceDir:        instanceDir,
		AgentsDir:          agentsDir,
		AgentDir:           agentDir,
		DocsDir:            docsDir,
		Model:              model,
		AllowedTools:       allowedTools,
		PromptSourceMode:   firstNonEmpty(instanceManifest.Prompting.SourceMode, agentManifest.PromptSource, PromptSourcePromptStack),
		PromptStackState:   stack,
		MaterializedDocs:   materializedDocs,
		Delegation:         mergeDelegation(instanceManifest.Delegation, agentManifest.Delegation),
		Messaging:          instanceManifest.Messaging,
		RoleTemplates:      roleTemplates,
		Skills:             append([]string(nil), skills.Activated...),
		ChannelDefaults:    channels,
		Concurrency:        instanceManifest.Runtime,
		FeatureFlags:       DefaultFeatureSet(),
		ThinkingMode:       firstNonEmpty(agentManifest.Behavior.DefaultThinkingMode, cfg.Output.ThinkingMode),
		TimeoutMS:          timeoutMS,
		Enabled:            instanceManifest.Enabled && agentManifest.Enabled,
		LegacyConfig:       cfg,
		InstanceManifest:   instanceManifest,
		AgentManifest:      agentManifest,
	}
	if len(resolved.ChannelDefaults) == 0 {
		resolved.ChannelDefaults = instanceManifest.Channels
	}
	return resolved, nil
}

func CloneInstance(rootDir, sourceInstanceID, targetInstanceID string) (InstanceManifest, error) {
	manifest, err := LoadInstanceManifest(rootDir, sourceInstanceID)
	if err != nil {
		return InstanceManifest{}, err
	}
	manifest.InstanceID = targetInstanceID
	manifest.DisplayName = targetInstanceID
	manifest.CreatedAt = time.Time{}
	manifest.UpdatedAt = time.Time{}
	if err := SaveInstanceManifest(rootDir, manifest); err != nil {
		return InstanceManifest{}, err
	}
	agents, err := ListAgents(rootDir, sourceInstanceID)
	if err != nil {
		return InstanceManifest{}, err
	}
	for _, agentManifest := range agents {
		copied := agentManifest
		copied.CreatedAt = time.Time{}
		copied.UpdatedAt = time.Time{}
		if err := SaveAgentManifest(rootDir, targetInstanceID, copied); err != nil {
			return InstanceManifest{}, err
		}
		if err := copyAgentTree(rootDir, sourceInstanceID, targetInstanceID, copied.AgentID); err != nil {
			return InstanceManifest{}, err
		}
	}
	roleTemplates, _ := LoadInstanceRoles(rootDir, sourceInstanceID)
	_ = SaveInstanceRoles(rootDir, targetInstanceID, roleTemplates)
	skills, _ := LoadInstanceSkills(rootDir, sourceInstanceID)
	_ = SaveInstanceSkills(rootDir, targetInstanceID, skills)
	channels, _ := LoadInstanceChannels(rootDir, sourceInstanceID)
	_ = SaveInstanceChannels(rootDir, targetInstanceID, channels)
	return LoadInstanceManifest(rootDir, targetInstanceID)
}

func bootstrapInstanceManifestFromLegacyConfig(rootDir string) InstanceManifest {
	manifest := DefaultInstanceManifest(DefaultInstanceID)
	manifest.DisplayName = "Default"
	cfgPath := filepath.Join(ControlPlaneDir(rootDir), "config.json")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return manifest
	}
	manifest.Workspace.Root = firstNonEmpty(cfg.Workspace.Root, manifest.Workspace.Root)
	manifest.Runtime.DefaultAgentID = firstNonEmpty(cfg.Chat.DefaultAgentID, cfg.Discord.DefaultAgentID, cfg.Telegram.DefaultAgentID, manifest.Runtime.DefaultAgentID)
	manifest.Runtime.EnabledAgentIDs = collectEnabledAgentIDs(cfg, manifest.Runtime.DefaultAgentID)
	manifest.Runtime.MaxConcurrentRuns = maxInt(1, cfg.Engine.MaxConcurrentRuns)
	manifest.Delegation = DelegationPolicy{
		Enabled:            true,
		Mode:               strings.TrimSpace(cfg.Agents.DelegationMode),
		Threshold:          cfg.Agents.DelegationThreshold,
		CooldownIterations: cfg.Agents.DelegationCooldownIter,
		MaxDepth:           3,
		DelegationAgentID:  strings.TrimSpace(cfg.Agents.DelegationAgentID),
	}
	manifest.Messaging = MessagingPolicy{
		Enabled:                  cfg.Agents.AllowInterAgentMessaging,
		AllowInterAgentMessaging: cfg.Agents.AllowInterAgentMessaging,
		SharedInboxNamespace:     "instance",
		AllowCrossInstance:       false,
	}
	manifest.Skills = loadLegacySkills(rootDir, collectEnabledAgentIDs(cfg, manifest.Runtime.DefaultAgentID))
	manifest.Roles = RolesConfig{TemplateNames: roleTemplateNames(cfg.Agents.CustomRoleTemplates)}
	manifest.Channels = collectChannelRoutes(cfg)
	return manifest
}

func collectChannelRoutes(cfg config.Config) map[string]ChannelRoute {
	routes := map[string]ChannelRoute{}
	if strings.TrimSpace(cfg.Chat.DefaultAgentID) != "" {
		routes["dashboard"] = ChannelRoute{DefaultAgentID: strings.TrimSpace(cfg.Chat.DefaultAgentID)}
	}
	if strings.TrimSpace(cfg.Discord.DefaultAgentID) != "" {
		routes["discord"] = ChannelRoute{DefaultAgentID: strings.TrimSpace(cfg.Discord.DefaultAgentID)}
	}
	if strings.TrimSpace(cfg.Telegram.DefaultAgentID) != "" {
		routes["telegram"] = ChannelRoute{DefaultAgentID: strings.TrimSpace(cfg.Telegram.DefaultAgentID)}
	}
	return routes
}

func ensureBootstrappedAgent(rootDir, instanceID, agentID string) error {
	manifestPath, err := AgentManifestPath(rootDir, instanceID, agentID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
		manifest := bootstrapAgentManifestFromLegacyConfig(rootDir, agentID)
		if legacyDir, legacyErr := LegacyAgentDir(rootDir, agentID); legacyErr == nil {
			if _, statErr := os.Stat(legacyDir); statErr == nil {
				manifest.LegacySourcePath = legacyDir
			}
		}
		if err := SaveAgentManifest(rootDir, instanceID, manifest); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	_, err = SeedLegacyAgentDocs(rootDir, instanceID, agentID)
	return err
}

func bootstrapAgentManifestFromLegacyConfig(rootDir, agentID string) AgentManifest {
	manifest := DefaultAgentManifest(agentID)
	cfg, err := config.LoadOrDefault(filepath.Join(ControlPlaneDir(rootDir), "config.json"))
	if err != nil {
		return manifest
	}
	profile, ok := cfg.Agents.Profiles[agentID]
	if ok {
		manifest.Model = profile.Model
		manifest.Behavior.SelfImprovement = profile.SelfImprovement
		if profile.Enabled != nil {
			manifest.Enabled = *profile.Enabled
		}
	}
	manifest.Restrictions = AgentRestrictions{
		AllowedTools:      append([]string(nil), cfg.Agents.SubAgentDefaults.AllowedTools...),
		MaxToolIterations: cfg.Agents.SubAgentDefaults.MaxToolIterations,
		TimeoutMS:         cfg.Agents.SubAgentDefaults.TimeoutMS,
	}
	manifest.Communication = AgentCommunication{AllowCrossInstance: false}
	manifest.PromptSource = PromptSourcePromptStack
	return manifest
}

func listLegacyAgentIDs(rootDir string) ([]string, error) {
	entries, err := os.ReadDir(LegacyAgentsDir(rootDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := ValidateAgentID(entry.Name()); err != nil {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

func loadLegacyRoleTemplates(rootDir string) []roles.RoleTemplate {
	cfg, err := config.LoadOrDefault(filepath.Join(ControlPlaneDir(rootDir), "config.json"))
	if err != nil {
		return nil
	}
	out := make([]roles.RoleTemplate, len(cfg.Agents.CustomRoleTemplates))
	copy(out, cfg.Agents.CustomRoleTemplates)
	return out
}

func roleTemplateNames(templates []roles.RoleTemplate) []string {
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

func loadLegacySkills(rootDir string, agentIDs []string) SkillsConfig {
	activated := map[string]struct{}{}
	for _, agentID := range agentIDs {
		legacyDir, err := LegacyAgentDir(rootDir, agentID)
		if err != nil {
			continue
		}
		toolsPath := filepath.Join(legacyDir, "TOOLS.md")
		data, err := os.ReadFile(toolsPath)
		if err != nil {
			continue
		}
		for _, skill := range extractSkillsFromToolsDoc(string(data)) {
			activated[skill] = struct{}{}
		}
	}
	out := make([]string, 0, len(activated))
	for skill := range activated {
		out = append(out, skill)
	}
	sort.Strings(out)
	return SkillsConfig{Activated: out}
}

func extractSkillsFromToolsDoc(content string) []string {
	lines := strings.Split(content, "\n")
	out := []string{}
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "<!-- OPENCLAWSSY_ACTIVATED_SKILLS_START -->" {
			inBlock = true
			continue
		}
		if trimmed == "<!-- OPENCLAWSSY_ACTIVATED_SKILLS_END -->" {
			break
		}
		if !inBlock || !strings.HasPrefix(trimmed, "-") {
			continue
		}
		skill := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if skill != "" {
			out = append(out, skill)
		}
	}
	return out
}

func collectEnabledAgentIDs(cfg config.Config, fallback string) []string {
	ids := map[string]struct{}{}
	for _, id := range cfg.Agents.EnabledAgentIDs {
		if validated, err := ValidateAgentID(id); err == nil {
			ids[validated] = struct{}{}
		}
	}
	for id := range cfg.Agents.Profiles {
		if validated, err := ValidateAgentID(id); err == nil {
			ids[validated] = struct{}{}
		}
	}
	for _, id := range []string{cfg.Chat.DefaultAgentID, cfg.Discord.DefaultAgentID, cfg.Telegram.DefaultAgentID, fallback} {
		if validated, err := ValidateAgentID(id); err == nil {
			ids[validated] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func ensurePromptStackInitialized(rootDir, instanceID, agentID string, store *promptstack.VersionStore) error {
	stack, err := store.GetCurrent(agentID)
	if err != nil {
		return err
	}
	if promptStackHasPersistedContent(stack) {
		return nil
	}
	seed := seedPromptStackFromDocs(rootDir, instanceID, agentID)
	for _, layer := range seed.LayersInOrder() {
		if strings.TrimSpace(layer.Content) == "" {
			continue
		}
		if _, err := store.UpdateLayer(agentID, layer.LayerID, layer.Content); err != nil {
			return err
		}
	}
	return nil
}

func promptStackHasPersistedContent(stack promptstack.PromptStack) bool {
	for _, layer := range stack.LayersInOrder() {
		if layer.Version > 0 || strings.TrimSpace(layer.Content) != "" {
			return true
		}
	}
	return false
}

func seedPromptStackFromDocs(rootDir, instanceID, agentID string) promptstack.PromptStack {
	stack := promptstack.NewPromptStack()
	seedLayers := map[string]string{
		promptstack.LayerGlobalOperatorPolicy: readPromptSeedDoc(rootDir, instanceID, agentID, "SPECPLAN.md"),
		promptstack.LayerAgentIdentity:        readPromptSeedDoc(rootDir, instanceID, agentID, "SOUL.md"),
		promptstack.LayerToolSafetyRules: joinPromptSeedDocs(
			readPromptSeedDoc(rootDir, instanceID, agentID, "RULES.md"),
			readPromptSeedDoc(rootDir, instanceID, agentID, "TOOLS.md"),
		),
		promptstack.LayerDelegationPolicy: readPromptSeedDoc(rootDir, instanceID, agentID, "DEVPLAN.md"),
		promptstack.LayerSessionOverlay: joinPromptSeedDocs(
			readPromptSeedDoc(rootDir, instanceID, agentID, "HEARTBEAT.md"),
			readPromptSeedDoc(rootDir, instanceID, agentID, "HANDOFF.md"),
		),
	}
	for _, layerID := range promptstack.LayerIDs() {
		_ = stack.SetLayer(promptstack.PromptLayer{LayerID: layerID, Content: seedLayers[layerID]})
	}
	return stack
}

func readPromptSeedDoc(rootDir, instanceID, agentID, name string) string {
	canonical, ok := NormalizeAgentDocName(name)
	if !ok {
		return ""
	}
	instanceDocPath, err := AgentDocPath(rootDir, instanceID, agentID, canonical)
	if err == nil {
		if data, readErr := os.ReadFile(instanceDocPath); readErr == nil {
			return strings.TrimSpace(string(data))
		}
	}
	legacyDir, err := LegacyAgentDir(rootDir, agentID)
	if err != nil {
		return ""
	}
	if data, readErr := os.ReadFile(filepath.Join(legacyDir, canonical)); readErr == nil {
		return strings.TrimSpace(string(data))
	}
	if canonical == "HANDOFF.md" {
		if data, readErr := os.ReadFile(filepath.Join(legacyDir, "HEARTBEAT.md")); readErr == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func joinPromptSeedDocs(parts ...string) string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n\n")
}

func mergeDelegation(instancePolicy, agentPolicy DelegationPolicy) DelegationPolicy {
	merged := instancePolicy
	if agentPolicy.Mode != "" {
		merged.Mode = agentPolicy.Mode
	}
	if agentPolicy.Threshold > 0 {
		merged.Threshold = agentPolicy.Threshold
	}
	if agentPolicy.CooldownIterations > 0 {
		merged.CooldownIterations = agentPolicy.CooldownIterations
	}
	if agentPolicy.MaxDepth > 0 {
		merged.MaxDepth = agentPolicy.MaxDepth
	}
	if agentPolicy.DelegationAgentID != "" {
		merged.DelegationAgentID = agentPolicy.DelegationAgentID
	}
	if agentPolicy.DefaultRole != "" {
		merged.DefaultRole = agentPolicy.DefaultRole
	}
	if agentPolicy.Enabled {
		merged.Enabled = true
	}
	return merged
}

func copyAgentTree(rootDir, sourceInstanceID, targetInstanceID, agentID string) error {
	sourceDocsDir, err := AgentDocsDir(rootDir, sourceInstanceID, agentID)
	if err != nil {
		return err
	}
	targetDocsDir, err := AgentDocsDir(rootDir, targetInstanceID, agentID)
	if err != nil {
		return err
	}
	if err := copyDirContents(sourceDocsDir, targetDocsDir); err != nil {
		return err
	}
	sourceStackDir, err := AgentPromptStackDir(rootDir, sourceInstanceID, agentID)
	if err == nil {
		targetStackDir, targetErr := AgentPromptStackDir(rootDir, targetInstanceID, agentID)
		if targetErr == nil {
			_ = copyDirContents(sourceStackDir, targetStackDir)
		}
	}
	sourceIdentity, err := AgentIdentityPath(rootDir, sourceInstanceID, agentID)
	if err == nil {
		targetIdentity, targetErr := AgentIdentityPath(rootDir, targetInstanceID, agentID)
		if targetErr == nil {
			_ = copyFileIfExists(sourceIdentity, targetIdentity)
		}
	}
	return nil
}

func copyDirContents(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := copyFileIfExists(filepath.Join(srcDir, entry.Name()), filepath.Join(dstDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFileIfExists(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return fsutil.WriteFileAtomic(dstPath, data, 0o600)
}

func readLegacyDocOrScaffold(legacyDir, name string, scaffold map[string]string) ([]byte, error) {
	srcPath := filepath.Join(legacyDir, name)
	data, err := os.ReadFile(srcPath)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	content, ok := scaffold[name]
	if !ok {
		return nil, nil
	}
	return []byte(content), nil
}

func copyLegacyIdentityIfPresent(rootDir, instanceID, agentID, legacyDir string) error {
	srcPath := filepath.Join(legacyDir, identityFileName)
	data, err := os.ReadFile(srcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	dstPath, err := AgentIdentityPath(rootDir, instanceID, agentID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dstPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fsutil.WriteFileAtomic(dstPath, data, 0o600)
}

func loadJSONFile(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	buf, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	buf = append(buf, '\n')
	return fsutil.WriteFileAtomic(path, buf, 0o600)
}

func mustAgentsDir(rootDir, instanceID string) string {
	path, err := AgentsDir(rootDir, instanceID)
	if err != nil {
		return filepath.Join(InstancesDir(rootDir), strings.TrimSpace(instanceID), "agents")
	}
	return path
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
