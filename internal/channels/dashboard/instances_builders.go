package dashboard

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"openclawssy/internal/config"
)

type instanceBuildInput struct {
	ID               string
	Name             string
	Description      string
	Template         string
	Source           string
	SourceInstanceID string
	Config           config.Config
	CreatedAt        string
	UpdatedAt        string
}

type wizardInstanceRequest struct {
	TemplateID     string `json:"template_id"`
	InstanceID     string `json:"instance_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	DefaultAgentID string `json:"default_agent_id"`
	ModelProvider  string `json:"model_provider"`
	ModelName      string `json:"model_name"`
}

type wizardAgentRequest struct {
	InstanceID      string `json:"instance_id"`
	AgentID         string `json:"agent_id"`
	TemplateID      string `json:"template_id"`
	Enabled         *bool  `json:"enabled"`
	SelfImprovement *bool  `json:"self_improvement"`
	ModelProvider   string `json:"model_provider"`
	ModelName       string `json:"model_name"`
	ModelTimeoutMS  *int   `json:"model_timeout_ms"`
}

type instanceWizardTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type agentWizardTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type instanceWizardPlan struct {
	Instance   dashboardInstance `json:"instance"`
	Operations []string          `json:"operations"`
}

type agentWizardPlan struct {
	InstanceID string              `json:"instance_id"`
	AgentID    string              `json:"agent_id"`
	Profile    config.AgentProfile `json:"profile"`
	Operations []string            `json:"operations"`
	TemplateID string              `json:"template_id"`
}

func availableInstanceWizardTemplates() []instanceWizardTemplate {
	return []instanceWizardTemplate{
		{ID: "blank", Name: "Blank", Description: "Start from the default safe configuration."},
		{ID: "chat-assistant", Name: "Chat Assistant", Description: "Tune the default config for conversational use with a primary agent."},
		{ID: "automation", Name: "Automation", Description: "Tune the default config for scheduled and operator-driven workflows."},
	}
}

func availableAgentWizardTemplates() []agentWizardTemplate {
	return []agentWizardTemplate{
		{ID: "general", Name: "General", Description: "A general-purpose agent profile with explicit enablement."},
		{ID: "research", Name: "Research", Description: "A research-oriented profile with a longer model timeout."},
		{ID: "operator", Name: "Operator", Description: "An operations profile tuned for longer-running tasks."},
	}
}

func buildDashboardInstance(input instanceBuildInput) (dashboardInstance, error) {
	instanceID, err := normalizeInstanceID(input.ID)
	if err != nil {
		return dashboardInstance{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = instanceID
	}
	if len(name) > maxInstanceNameLen {
		return dashboardInstance{}, fmt.Errorf("name exceeds %d characters", maxInstanceNameLen)
	}
	description := strings.TrimSpace(input.Description)
	if len(description) > maxInstanceDescriptionLen {
		return dashboardInstance{}, fmt.Errorf("description exceeds %d characters", maxInstanceDescriptionLen)
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = "custom"
	}
	if len(template) > maxInstanceTemplateLen {
		return dashboardInstance{}, fmt.Errorf("template exceeds %d characters", maxInstanceTemplateLen)
	}
	source := strings.TrimSpace(input.Source)
	if len(source) > maxInstanceTemplateLen {
		return dashboardInstance{}, fmt.Errorf("source exceeds %d characters", maxInstanceTemplateLen)
	}
	clonedCfg, err := cloneDashboardConfig(input.Config)
	if err != nil {
		return dashboardInstance{}, err
	}
	if err := clonedCfg.Validate(); err != nil {
		return dashboardInstance{}, err
	}
	createdAt := strings.TrimSpace(input.CreatedAt)
	updatedAt := strings.TrimSpace(input.UpdatedAt)
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if updatedAt == "" {
		updatedAt = createdAt
	}
	return dashboardInstance{
		ID:               instanceID,
		Name:             name,
		Description:      description,
		Template:         template,
		Source:           source,
		SourceInstanceID: strings.TrimSpace(input.SourceInstanceID),
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		Config:           clonedCfg,
	}, nil
}

func buildInstanceAgentConfig(cfg config.Config, agentID string, profile config.AgentProfile) (config.Config, config.AgentProfile, error) {
	normalizedAgentID, err := normalizeDashboardAgentID(agentID)
	if err != nil {
		return config.Config{}, config.AgentProfile{}, err
	}
	clonedCfg, err := cloneDashboardConfig(cfg)
	if err != nil {
		return config.Config{}, config.AgentProfile{}, err
	}
	if clonedCfg.Agents.Profiles == nil {
		clonedCfg.Agents.Profiles = map[string]config.AgentProfile{}
	}
	clonedCfg.Agents.Profiles[normalizedAgentID] = profile
	if !containsString(clonedCfg.Agents.EnabledAgentIDs, normalizedAgentID) {
		clonedCfg.Agents.EnabledAgentIDs = append(clonedCfg.Agents.EnabledAgentIDs, normalizedAgentID)
	}
	clonedCfg.ApplyDefaults()
	if err := clonedCfg.Validate(); err != nil {
		return config.Config{}, config.AgentProfile{}, err
	}
	return clonedCfg, clonedCfg.Agents.Profiles[normalizedAgentID], nil
}

func deleteInstanceAgentConfig(cfg config.Config, agentID string) (config.Config, error) {
	normalizedAgentID, err := normalizeDashboardAgentID(agentID)
	if err != nil {
		return config.Config{}, err
	}
	clonedCfg, err := cloneDashboardConfig(cfg)
	if err != nil {
		return config.Config{}, err
	}
	if _, ok := clonedCfg.Agents.Profiles[normalizedAgentID]; !ok {
		return config.Config{}, errors.New("agent not found")
	}
	delete(clonedCfg.Agents.Profiles, normalizedAgentID)
	filtered := make([]string, 0, len(clonedCfg.Agents.EnabledAgentIDs))
	for _, existing := range clonedCfg.Agents.EnabledAgentIDs {
		if strings.TrimSpace(existing) == normalizedAgentID {
			continue
		}
		filtered = append(filtered, existing)
	}
	clonedCfg.Agents.EnabledAgentIDs = filtered
	clonedCfg.ApplyDefaults()
	if err := clonedCfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return clonedCfg, nil
}

func buildWizardInstancePlan(req wizardInstanceRequest) (instanceWizardPlan, error) {
	template, err := lookupInstanceWizardTemplate(req.TemplateID)
	if err != nil {
		return instanceWizardPlan{}, err
	}
	baseCfg := config.Default()
	switch template.ID {
	case "chat-assistant":
		baseCfg.Chat.Enabled = true
		defaultAgentID := strings.TrimSpace(req.DefaultAgentID)
		if defaultAgentID == "" {
			defaultAgentID = "default"
		}
		normalizedAgentID, err := normalizeDashboardAgentID(defaultAgentID)
		if err != nil {
			return instanceWizardPlan{}, err
		}
		baseCfg.Chat.DefaultAgentID = normalizedAgentID
		baseCfg.Discord.DefaultAgentID = normalizedAgentID
		baseCfg.Telegram.DefaultAgentID = normalizedAgentID
	case "automation":
		baseCfg.Chat.Enabled = false
		baseCfg.Scheduler.CatchUp = true
		baseCfg.Agents.AllowInterAgentMessaging = true
	}
	if provider := strings.ToLower(strings.TrimSpace(req.ModelProvider)); provider != "" {
		baseCfg.Model.Provider = provider
	}
	if name := strings.TrimSpace(req.ModelName); name != "" {
		baseCfg.Model.Name = name
	}
	instance, err := buildDashboardInstance(instanceBuildInput{
		ID:          req.InstanceID,
		Name:        req.Name,
		Description: req.Description,
		Template:    template.ID,
		Source:      "wizard",
		Config:      baseCfg,
	})
	if err != nil {
		return instanceWizardPlan{}, err
	}
	operations := []string{
		"create instance metadata",
		"persist instance config snapshot",
	}
	if template.ID == "chat-assistant" {
		operations = append(operations, "set chat channel default agent")
	}
	if template.ID == "automation" {
		operations = append(operations, "preserve scheduler-first defaults")
	}
	return instanceWizardPlan{Instance: instance, Operations: operations}, nil
}

func buildWizardAgentPlan(instance dashboardInstance, req wizardAgentRequest) (agentWizardPlan, error) {
	template, err := lookupAgentWizardTemplate(req.TemplateID)
	if err != nil {
		return agentWizardPlan{}, err
	}
	profile := config.AgentProfile{}
	switch template.ID {
	case "general":
		profile.Enabled = boolPtr(true)
	case "research":
		profile.Enabled = boolPtr(true)
		profile.Model.TimeoutMS = 180000
	case "operator":
		profile.Enabled = boolPtr(true)
		profile.Model.TimeoutMS = 240000
	}
	if req.Enabled != nil {
		profile.Enabled = boolPtr(*req.Enabled)
	}
	if req.SelfImprovement != nil {
		profile.SelfImprovement = *req.SelfImprovement
	}
	if provider := strings.ToLower(strings.TrimSpace(req.ModelProvider)); provider != "" {
		profile.Model.Provider = provider
	}
	if name := strings.TrimSpace(req.ModelName); name != "" {
		profile.Model.Name = name
	}
	if req.ModelTimeoutMS != nil {
		profile.Model.TimeoutMS = *req.ModelTimeoutMS
	}
	_, normalizedProfile, err := buildInstanceAgentConfig(instance.Config, req.AgentID, profile)
	if err != nil {
		return agentWizardPlan{}, err
	}
	normalizedAgentID, err := normalizeDashboardAgentID(req.AgentID)
	if err != nil {
		return agentWizardPlan{}, err
	}
	operations := []string{
		"normalize agent profile",
		"validate instance config with agent overrides",
	}
	return agentWizardPlan{
		InstanceID: instance.ID,
		AgentID:    normalizedAgentID,
		Profile:    normalizedProfile,
		Operations: operations,
		TemplateID: template.ID,
	}, nil
}

func lookupInstanceWizardTemplate(raw string) (instanceWizardTemplate, error) {
	target := strings.TrimSpace(raw)
	for _, template := range availableInstanceWizardTemplates() {
		if template.ID == target {
			return template, nil
		}
	}
	return instanceWizardTemplate{}, errors.New("unknown instance template")
}

func lookupAgentWizardTemplate(raw string) (agentWizardTemplate, error) {
	target := strings.TrimSpace(raw)
	for _, template := range availableAgentWizardTemplates() {
		if template.ID == target {
			return template, nil
		}
	}
	return agentWizardTemplate{}, errors.New("unknown agent template")
}

func boolPtr(v bool) *bool {
	return &v
}
