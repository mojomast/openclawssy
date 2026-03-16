package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/instances"
)

type controlPlaneFeaturesPatch struct {
	InstanceControl *bool `json:"instance_control"`
	InstanceAgents  *bool `json:"instance_agents"`
	Wizard          *bool `json:"wizard"`
}

type createInstanceRequest struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Template    string         `json:"template"`
	Config      *config.Config `json:"config"`
}

type updateInstanceRequest struct {
	ID          *string        `json:"id"`
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Template    *string        `json:"template"`
	Config      *config.Config `json:"config"`
}

type cloneInstanceRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type upsertInstanceAgentRequest struct {
	AgentID string              `json:"agent_id"`
	Profile config.AgentProfile `json:"profile"`
}

func (h *Handler) handleControlPlaneFeatures(w http.ResponseWriter, r *http.Request) {
	store, err := h.loadControlPlaneStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "control_plane.load_failed", err.Error(), nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"features": store.Features, "updated_at": store.UpdatedAt})
	case http.MethodPatch:
		var req controlPlaneFeaturesPatch
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeDashboardError(w, http.StatusBadRequest, "control_plane.invalid_json", "invalid json body", nil)
			return
		}
		if req.InstanceControl != nil {
			store.Features.InstanceControl = *req.InstanceControl
		}
		if req.InstanceAgents != nil {
			store.Features.InstanceAgents = *req.InstanceAgents
		}
		if req.Wizard != nil {
			store.Features.Wizard = *req.Wizard
		}
		if err := h.saveControlPlaneStore(store); err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "control_plane.save_failed", err.Error(), nil)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "features": store.Features})
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) handleInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listInstances(w)
	case http.MethodPost:
		h.createInstance(w, r)
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) listInstances(w http.ResponseWriter) {
	projected, activeInstanceID, err := h.listProjectedInstances()
	if err != nil {
		store, fallbackErr := h.loadControlPlaneStore()
		if fallbackErr != nil {
			writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
			return
		}
		summaries := make([]map[string]any, 0, len(store.Instances))
		for _, instance := range store.Instances {
			summaries = append(summaries, instanceSummary(instance, store.ActiveInstanceID))
		}
		writeJSON(w, map[string]any{
			"instances":          summaries,
			"active_instance_id": store.ActiveInstanceID,
			"count":              len(summaries),
		})
		return
	}
	summaries := make([]map[string]any, 0, len(projected))
	for _, instance := range projected {
		summaries = append(summaries, instanceSummary(instance, activeInstanceID))
	}
	writeJSON(w, map[string]any{
		"instances":          summaries,
		"active_instance_id": activeInstanceID,
		"count":              len(summaries),
	})
}

func (h *Handler) createInstance(w http.ResponseWriter, r *http.Request) {
	var req createInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
		return
	}
	instanceCfg := config.Default()
	if req.Config != nil {
		instanceCfg = *req.Config
	}
	instance, err := buildDashboardInstance(instanceBuildInput{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Template:    req.Template,
		Source:      "api",
		Config:      instanceCfg,
	})
	if err != nil {
		h.writeInstanceError(w, err)
		return
	}
	exists, err := h.projectedInstanceExists(instance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	if exists {
		writeDashboardError(w, http.StatusConflict, "instances.duplicate_id", "instance already exists", nil)
		return
	}
	if err := h.saveProjectedInstance(instance); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	activeInstanceID, _ := instances.LoadActiveInstanceID(h.rootDir)
	projected, err := h.loadProjectedInstance(instance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"ok": true, "instance": instancePayload(projected, activeInstanceID)})
}

func (h *Handler) handleActiveInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	activeInstanceID, err := instances.LoadActiveInstanceID(h.rootDir)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	instance, err := h.loadProjectedInstance(activeInstanceID)
	if err != nil {
		store, fallbackErr := h.loadControlPlaneStore()
		if fallbackErr == nil {
			if strings.TrimSpace(store.ActiveInstanceID) == "" {
				writeDashboardError(w, http.StatusNotFound, "instances.active_not_found", "active instance not set", nil)
				return
			}
			fallback, _, ok := store.instanceByID(store.ActiveInstanceID)
			if ok {
				writeJSON(w, map[string]any{"instance": instancePayload(fallback, store.ActiveInstanceID)})
				return
			}
		}
		if errors.Is(err, os.ErrNotExist) {
			writeDashboardError(w, http.StatusNotFound, "instances.active_not_found", "active instance not found", nil)
			return
		}
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"instance": instancePayload(instance, activeInstanceID)})
}

func (h *Handler) handleBootstrapInstanceFromCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	var req createInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
		return
	}
	currentCfg, err := h.loadDashboardConfig()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}
	instance, err := buildDashboardInstance(instanceBuildInput{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Template:    firstNonEmpty(strings.TrimSpace(req.Template), "bootstrap"),
		Source:      "current",
		Config:      currentCfg,
	})
	if err != nil {
		h.writeInstanceError(w, err)
		return
	}
	exists, err := h.projectedInstanceExists(instance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	if exists {
		writeDashboardError(w, http.StatusConflict, "instances.duplicate_id", "instance already exists", nil)
		return
	}
	if err := h.saveProjectedInstance(instance); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	activated := false
	if !h.hasCanonicalActiveInstanceSelection() {
		if _, err := instances.ActivateInstance(h.rootDir, instance.ID); err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
			return
		}
		activated = true
	}
	activeInstanceID, _ := instances.LoadActiveInstanceID(h.rootDir)
	projected, err := h.loadProjectedInstance(instance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"ok":        true,
		"activated": activated,
		"instance":  instancePayload(projected, activeInstanceID),
	})
}

func (h *Handler) handleInstanceByID(w http.ResponseWriter, r *http.Request) {
	instanceID, segments, ok := parseInstanceAdminRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if len(segments) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.getInstance(w, instanceID)
		case http.MethodPut:
			h.updateInstance(w, r, instanceID)
		case http.MethodDelete:
			h.deleteInstance(w, instanceID)
		default:
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		}
		return
	}

	switch segments[0] {
	case "activate":
		if len(segments) != 1 || r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.activateInstance(w, instanceID)
	case "clone":
		if len(segments) != 1 || r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.cloneInstance(w, r, instanceID)
	case "agents":
		if len(segments) >= 3 && segments[2] == "prompt-stack" {
			agentID, err := normalizeDashboardAgentID(segments[1])
			if err != nil {
				writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
				return
			}
			h.handlePromptStackAPI(w, r, instanceID, agentID, segments[3:])
			return
		}
		h.handleInstanceAgents(w, r, instanceID, segments[1:])
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) getInstance(w http.ResponseWriter, instanceID string) {
	activeInstanceID, _ := instances.LoadActiveInstanceID(h.rootDir)
	instance, err := h.loadProjectedInstance(instanceID)
	if err != nil {
		store, fallback, _, ok := h.loadSingleInstance(w, instanceID)
		if !ok {
			return
		}
		writeJSON(w, map[string]any{"instance": instancePayload(fallback, store.ActiveInstanceID)})
		return
	}
	writeJSON(w, map[string]any{"instance": instancePayload(instance, activeInstanceID)})
}

func (h *Handler) updateInstance(w http.ResponseWriter, r *http.Request, instanceID string) {
	existing, err := h.loadProjectedInstance(instanceID)
	if err != nil {
		if _, _, _, ok := h.loadSingleInstance(w, instanceID); !ok {
			return
		}
		h.writeInstanceError(w, err)
		return
	}
	var req updateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
		return
	}
	if req.ID != nil {
		normalized, err := normalizeInstanceID(*req.ID)
		if err != nil {
			h.writeInstanceError(w, err)
			return
		}
		if normalized != instanceID {
			writeDashboardError(w, http.StatusBadRequest, "instances.id_mismatch", "body id must match path id", nil)
			return
		}
	}
	updatedCfg := existing.Config
	if req.Config != nil {
		updatedCfg = *req.Config
	}
	updated := existing
	if req.Name != nil {
		updated.Name = *req.Name
	}
	if req.Description != nil {
		updated.Description = *req.Description
	}
	if req.Template != nil {
		updated.Template = *req.Template
	}
	updated.Config = updatedCfg
	updatedInstance, err := buildDashboardInstance(instanceBuildInput{
		ID:               updated.ID,
		Name:             updated.Name,
		Description:      updated.Description,
		Template:         updated.Template,
		Source:           existing.Source,
		SourceInstanceID: existing.SourceInstanceID,
		Config:           updated.Config,
		CreatedAt:        existing.CreatedAt,
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		h.writeInstanceError(w, err)
		return
	}
	if err := h.saveProjectedInstance(updatedInstance); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	activeInstanceID, _ := instances.LoadActiveInstanceID(h.rootDir)
	projected, err := h.loadProjectedInstance(updatedInstance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "instance": instancePayload(projected, activeInstanceID)})
}

func (h *Handler) deleteInstance(w http.ResponseWriter, instanceID string) {
	activeInstanceID, err := instances.LoadActiveInstanceID(h.rootDir)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	if activeInstanceID == instanceID {
		writeDashboardError(w, http.StatusConflict, "instances.active_conflict", "cannot delete the active instance", nil)
		return
	}
	if err := instances.DeleteInstance(h.rootDir, instanceID); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "deleted": instanceID})
}

func (h *Handler) activateInstance(w http.ResponseWriter, instanceID string) {
	instance, err := h.loadProjectedInstance(instanceID)
	if err != nil {
		if _, _, _, ok := h.loadSingleInstance(w, instanceID); !ok {
			return
		}
		h.writeInstanceError(w, err)
		return
	}
	if err := saveDashboardConfig(filepath.Join(h.rootDir, ".openclawssy", "config.json"), instance.Config); err != nil {
		writeConfigValidationError(w, err, instance.Config)
		return
	}
	active, err := instances.ActivateInstance(h.rootDir, instanceID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	projected, err := h.projectDashboardInstance(active)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "instance": instancePayload(projected, active.InstanceID)})
}

func (h *Handler) cloneInstance(w http.ResponseWriter, r *http.Request, sourceInstanceID string) {
	existing, err := h.loadProjectedInstance(sourceInstanceID)
	if err != nil {
		if _, _, _, ok := h.loadSingleInstance(w, sourceInstanceID); !ok {
			return
		}
		h.writeInstanceError(w, err)
		return
	}
	var req cloneInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
		return
	}
	cloned, err := buildDashboardInstance(instanceBuildInput{
		ID:               req.ID,
		Name:             req.Name,
		Description:      firstNonEmpty(strings.TrimSpace(req.Description), existing.Description),
		Template:         existing.Template,
		Source:           "clone",
		SourceInstanceID: existing.ID,
		Config:           existing.Config,
	})
	if err != nil {
		h.writeInstanceError(w, err)
		return
	}
	exists, err := h.projectedInstanceExists(cloned.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	if exists {
		writeDashboardError(w, http.StatusConflict, "instances.duplicate_id", "instance already exists", nil)
		return
	}
	if err := h.saveProjectedInstance(cloned); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	activeInstanceID, _ := instances.LoadActiveInstanceID(h.rootDir)
	projected, err := h.loadProjectedInstance(cloned.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"ok": true, "instance": instancePayload(projected, activeInstanceID)})
}

func (h *Handler) handleInstanceAgents(w http.ResponseWriter, r *http.Request, instanceID string, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.listInstanceAgents(w, instanceID)
		case http.MethodPost:
			h.createInstanceAgent(w, r, instanceID)
		default:
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		}
		return
	}
	if len(segments) != 1 {
		http.NotFound(w, r)
		return
	}
	agentID, err := normalizeDashboardAgentID(segments[0])
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getInstanceAgent(w, instanceID, agentID)
	case http.MethodPut:
		h.updateInstanceAgent(w, r, instanceID, agentID)
	case http.MethodDelete:
		h.deleteInstanceAgent(w, instanceID, agentID)
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) listInstanceAgents(w http.ResponseWriter, instanceID string) {
	agents, err := instances.ListAgents(h.rootDir, instanceID)
	if err != nil {
		if _, _, _, ok := h.loadSingleInstance(w, instanceID); !ok {
			return
		}
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	payload := make([]map[string]any, 0, len(agents))
	for _, agent := range agents {
		payload = append(payload, map[string]any{"agent_id": agent.AgentID, "profile": dashboardAgentProfile(agent)})
	}
	writeJSON(w, map[string]any{"instance_id": instanceID, "agents": payload, "count": len(payload)})
}

func (h *Handler) createInstanceAgent(w http.ResponseWriter, r *http.Request, instanceID string) {
	if _, err := h.loadProjectedInstance(instanceID); err != nil {
		if _, _, _, ok := h.loadSingleInstance(w, instanceID); !ok {
			return
		}
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	var req upsertInstanceAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
		return
	}
	normalizedAgentID, err := normalizeDashboardAgentID(req.AgentID)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
		return
	}
	if _, err := instances.LoadAgentManifest(h.rootDir, instanceID, normalizedAgentID); err == nil {
		writeDashboardError(w, http.StatusConflict, "instances.agent_exists", "agent already exists", nil)
		return
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	h.persistInstanceAgent(w, instanceID, normalizedAgentID, req.Profile, http.StatusCreated)
}

func (h *Handler) getInstanceAgent(w http.ResponseWriter, instanceID, agentID string) {
	agent, err := instances.LoadAgentManifest(h.rootDir, instanceID, agentID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeDashboardError(w, http.StatusNotFound, "instances.agent_not_found", "agent not found", nil)
			return
		}
		writeDashboardError(w, http.StatusNotFound, "instances.agent_not_found", "agent not found", nil)
		return
	}
	writeJSON(w, map[string]any{"instance_id": instanceID, "agent": map[string]any{"agent_id": agentID, "profile": dashboardAgentProfile(agent)}})
}

func (h *Handler) updateInstanceAgent(w http.ResponseWriter, r *http.Request, instanceID, agentID string) {
	if _, err := instances.LoadAgentManifest(h.rootDir, instanceID, agentID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeDashboardError(w, http.StatusNotFound, "instances.agent_not_found", "agent not found", nil)
			return
		}
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	var req upsertInstanceAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
		return
	}
	if bodyAgentID := strings.TrimSpace(req.AgentID); bodyAgentID != "" {
		normalized, err := normalizeDashboardAgentID(bodyAgentID)
		if err != nil {
			writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
			return
		}
		if normalized != agentID {
			writeDashboardError(w, http.StatusBadRequest, "instances.agent_id_mismatch", "body agent_id must match path agent_id", nil)
			return
		}
	}
	h.persistInstanceAgent(w, instanceID, agentID, req.Profile, http.StatusOK)
}

func (h *Handler) deleteInstanceAgent(w http.ResponseWriter, instanceID, agentID string) {
	if err := instances.DeleteAgent(h.rootDir, instanceID, agentID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeDashboardError(w, http.StatusNotFound, "instances.agent_not_found", "agent not found", nil)
			return
		}
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	instance, err := h.loadProjectedInstance(instanceID)
	if err == nil {
		filtered := make([]string, 0, len(instance.Config.Agents.EnabledAgentIDs))
		for _, enabledAgentID := range instance.Config.Agents.EnabledAgentIDs {
			if strings.TrimSpace(enabledAgentID) == agentID {
				continue
			}
			filtered = append(filtered, enabledAgentID)
		}
		instance.Config.Agents.EnabledAgentIDs = filtered
		if agentID == strings.TrimSpace(instance.Config.Chat.DefaultAgentID) {
			instance.Config.Chat.DefaultAgentID = firstNonEmpty(instance.Config.Discord.DefaultAgentID, instance.Config.Telegram.DefaultAgentID, "default")
		}
		if agentID == strings.TrimSpace(instance.Config.Discord.DefaultAgentID) {
			instance.Config.Discord.DefaultAgentID = firstNonEmpty(instance.Config.Chat.DefaultAgentID, instance.Config.Telegram.DefaultAgentID, "default")
		}
		if agentID == strings.TrimSpace(instance.Config.Telegram.DefaultAgentID) {
			instance.Config.Telegram.DefaultAgentID = firstNonEmpty(instance.Config.Chat.DefaultAgentID, instance.Config.Discord.DefaultAgentID, "default")
		}
		if err := h.saveProjectedInstance(instance); err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true, "instance_id": instanceID, "deleted": agentID})
}

func (h *Handler) persistInstanceAgent(w http.ResponseWriter, instanceID, agentID string, profile config.AgentProfile, status int) {
	instance, err := h.loadProjectedInstance(instanceID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	updatedCfg, normalizedProfile, err := buildInstanceAgentConfig(instance.Config, agentID, profile)
	if err != nil {
		h.writeInstanceError(w, err)
		return
	}
	normalizedAgentID, _ := normalizeDashboardAgentID(agentID)
	instance.Config = updatedCfg
	instance.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.saveProjectedInstance(instance); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	writeJSONStatus(w, status, map[string]any{
		"ok":          true,
		"instance_id": instance.ID,
		"agent":       map[string]any{"agent_id": normalizedAgentID, "profile": normalizedProfile},
	})
}

func (h *Handler) handleWizardTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	writeJSON(w, map[string]any{
		"instance_templates": availableInstanceWizardTemplates(),
		"agent_templates":    availableAgentWizardTemplates(),
	})
}

func (h *Handler) handleWizardInstancePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	var req wizardInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "wizard.invalid_json", "invalid json body", nil)
		return
	}
	plan, err := buildWizardInstancePlan(req)
	if err != nil {
		h.writeWizardError(w, err)
		return
	}
	writeJSON(w, map[string]any{"plan": map[string]any{"instance": instancePayload(plan.Instance, ""), "operations": plan.Operations}})
}

func (h *Handler) handleWizardInstanceCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	var req wizardInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "wizard.invalid_json", "invalid json body", nil)
		return
	}
	plan, err := buildWizardInstancePlan(req)
	if err != nil {
		h.writeWizardError(w, err)
		return
	}
	exists, err := h.projectedInstanceExists(plan.Instance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	if exists {
		writeDashboardError(w, http.StatusConflict, "instances.duplicate_id", "instance already exists", nil)
		return
	}
	if err := h.saveProjectedInstance(plan.Instance); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	activeInstanceID, _ := instances.LoadActiveInstanceID(h.rootDir)
	projected, err := h.loadProjectedInstance(plan.Instance.ID)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"ok": true, "instance": instancePayload(projected, activeInstanceID)})
}

func (h *Handler) handleWizardAgentPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	var req wizardAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "wizard.invalid_json", "invalid json body", nil)
		return
	}
	instance, ok := h.loadWizardInstanceTarget(w, req.InstanceID)
	if !ok {
		return
	}
	plan, err := buildWizardAgentPlan(instance, req)
	if err != nil {
		h.writeWizardError(w, err)
		return
	}
	writeJSON(w, map[string]any{"plan": plan})
}

func (h *Handler) handleWizardAgentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	var req wizardAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "wizard.invalid_json", "invalid json body", nil)
		return
	}
	instance, err := h.loadProjectedInstance(req.InstanceID)
	if err != nil {
		writeDashboardError(w, http.StatusNotFound, "instances.not_found", "instance not found", nil)
		return
	}
	plan, err := buildWizardAgentPlan(instance, req)
	if err != nil {
		h.writeWizardError(w, err)
		return
	}
	updatedCfg, normalizedProfile, err := buildInstanceAgentConfig(instance.Config, plan.AgentID, plan.Profile)
	if err != nil {
		h.writeWizardError(w, err)
		return
	}
	instance.Config = updatedCfg
	instance.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.saveProjectedInstance(instance); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.save_failed", err.Error(), nil)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"ok":          true,
		"instance_id": instance.ID,
		"agent":       map[string]any{"agent_id": plan.AgentID, "profile": normalizedProfile},
	})
}

func (h *Handler) loadSingleInstance(w http.ResponseWriter, instanceID string) (controlPlaneStore, dashboardInstance, int, bool) {
	store, err := h.loadControlPlaneStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
		return controlPlaneStore{}, dashboardInstance{}, -1, false
	}
	normalized, err := normalizeInstanceID(instanceID)
	if err != nil {
		h.writeInstanceError(w, err)
		return controlPlaneStore{}, dashboardInstance{}, -1, false
	}
	instance, index, ok := store.instanceByID(normalized)
	if !ok {
		writeDashboardError(w, http.StatusNotFound, "instances.not_found", "instance not found", nil)
		return controlPlaneStore{}, dashboardInstance{}, -1, false
	}
	return store, instance, index, true
}

func (h *Handler) loadWizardInstanceTarget(w http.ResponseWriter, instanceID string) (dashboardInstance, bool) {
	instance, err := h.loadProjectedInstance(instanceID)
	if err == nil {
		return instance, true
	}
	_, fallback, _, ok := h.loadSingleInstance(w, instanceID)
	return fallback, ok
}

func dashboardAgentProfile(agent instances.AgentManifest) config.AgentProfile {
	enabled := agent.Enabled
	return config.AgentProfile{
		Enabled:         boolPtr(enabled),
		Model:           agent.Model,
		SelfImprovement: agent.Behavior.SelfImprovement,
	}
}

func parseInstanceAdminRoute(requestPath string) (instanceID string, segments []string, ok bool) {
	suffix := strings.TrimPrefix(requestPath, "/api/admin/instances/")
	if suffix == requestPath {
		return "", nil, false
	}
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", nil, false
	}
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == "" {
			return "", nil, false
		}
	}
	return parts[0], parts[1:], true
}

func (h *Handler) writeInstanceError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "instance_id") || strings.Contains(lower, "invalid instance id"):
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_id", err.Error(), nil)
	case strings.Contains(lower, "name exceeds"):
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_name", err.Error(), nil)
	case strings.Contains(lower, "description exceeds"):
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_description", err.Error(), nil)
	case strings.Contains(lower, "template exceeds") || strings.Contains(lower, "source exceeds"):
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_template", err.Error(), nil)
	case strings.Contains(lower, "agent id"):
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", err.Error(), nil)
	case strings.Contains(lower, "unsupported") || strings.Contains(lower, "must be") || strings.Contains(lower, "cannot"):
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_config", err.Error(), nil)
	default:
		writeDashboardError(w, http.StatusInternalServerError, "instances.operation_failed", err.Error(), nil)
	}
}

func (h *Handler) writeWizardError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "unknown instance template"):
		writeDashboardError(w, http.StatusBadRequest, "wizard.unknown_instance_template", err.Error(), nil)
	case strings.Contains(lower, "unknown agent template"):
		writeDashboardError(w, http.StatusBadRequest, "wizard.unknown_agent_template", err.Error(), nil)
	default:
		h.writeInstanceError(w, err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
