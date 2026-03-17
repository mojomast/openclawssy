package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/instances"
	"openclawssy/internal/tools"
)

type controlPlaneFeaturesPatch struct {
	InstanceControl *bool `json:"instance_control"`
	InstanceAgents  *bool `json:"instance_agents"`
	Wizard          *bool `json:"wizard"`
	Eval            *bool `json:"eval"`
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

type dashboardInboxMessage struct {
	MessageID       string    `json:"message_id"`
	Status          string    `json:"status"`
	InstanceID      string    `json:"instance_id,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	FromAgentID     string    `json:"from_agent_id,omitempty"`
	ToAgentID       string    `json:"to_agent_id,omitempty"`
	Subject         string    `json:"subject,omitempty"`
	TaskID          string    `json:"task_id,omitempty"`
	Channel         string    `json:"channel,omitempty"`
	UserID          string    `json:"user_id,omitempty"`
	Message         string    `json:"message,omitempty"`
	RelatedRunID    string    `json:"related_run_id,omitempty"`
	Note            string    `json:"note,omitempty"`
	Error           string    `json:"error,omitempty"`
	Role            string    `json:"role,omitempty"`
	TS              time.Time `json:"ts"`
}

func (h *Handler) requireControlPlaneFeature(w http.ResponseWriter, code string, enabled bool, disabledCode, disabledMessage string) bool {
	if enabled {
		return true
	}
	writeDashboardError(w, http.StatusForbidden, disabledCode, disabledMessage, map[string]any{"feature": code})
	return false
}

func (h *Handler) requireWizardFeature(w http.ResponseWriter) bool {
	store, err := h.loadControlPlaneStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "control_plane.load_failed", err.Error(), nil)
		return false
	}
	return h.requireControlPlaneFeature(w, "wizard", store.Features.Wizard, "feature.wizard_disabled", "wizard routes are disabled")
}

func (h *Handler) requireInstanceControlFeature(w http.ResponseWriter) bool {
	store, err := h.loadControlPlaneStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "control_plane.load_failed", err.Error(), nil)
		return false
	}
	return h.requireControlPlaneFeature(w, "instance_control", store.Features.InstanceControl, "feature.instance_control_disabled", "instance control routes are disabled")
}

func (h *Handler) requireInstanceAgentsFeature(w http.ResponseWriter) bool {
	store, err := h.loadControlPlaneStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "control_plane.load_failed", err.Error(), nil)
		return false
	}
	return h.requireControlPlaneFeature(w, "instance_agents", store.Features.InstanceAgents, "feature.instance_agents_disabled", "instance agent routes are disabled")
}

func (h *Handler) requireEvalFeature(w http.ResponseWriter) bool {
	store, err := h.loadControlPlaneStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "control_plane.load_failed", err.Error(), nil)
		return false
	}
	return h.requireControlPlaneFeature(w, "eval", store.Features.Eval, "feature.eval_disabled", "eval routes are disabled")
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
		if req.Eval != nil {
			store.Features.Eval = *req.Eval
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

func (h *Handler) handleInstanceInbox(w http.ResponseWriter, r *http.Request) {
	if !h.requireInstanceAgentsFeature(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	instanceID := strings.TrimSpace(r.URL.Query().Get("instance_id"))
	if instanceID == "" {
		active, err := instances.LoadActiveInstanceID(h.rootDir)
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
			return
		}
		instanceID = active
	}
	instanceID, err := normalizeInstanceID(instanceID)
	if err != nil {
		h.writeInstanceError(w, err)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query(), 20, 200)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_pagination", err.Error(), nil)
		return
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentID != "" {
		agentID, err = normalizeDashboardAgentID(agentID)
		if err != nil {
			writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
			return
		}
	}
	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_unavailable", "failed to open inbox store", nil)
		return
	}
	messages, total, err := h.listInstanceInboxMessages(store, instanceID, agentID, limit, offset)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{
		"instance_id": instanceID,
		"agent_id":    agentID,
		"messages":    messages,
		"count":       len(messages),
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func (h *Handler) handleInstances(w http.ResponseWriter, r *http.Request) {
	if !h.requireInstanceControlFeature(w) {
		return
	}
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
	if !h.requireInstanceControlFeature(w) {
		return
	}
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
	if !h.requireInstanceControlFeature(w) {
		return
	}
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
		if !h.requireInstanceControlFeature(w) {
			return
		}
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
		if !h.requireInstanceControlFeature(w) {
			return
		}
		if len(segments) != 1 || r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.activateInstance(w, instanceID)
	case "clone":
		if !h.requireInstanceControlFeature(w) {
			return
		}
		if len(segments) != 1 || r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.cloneInstance(w, r, instanceID)
	case "agents":
		if !h.requireInstanceAgentsFeature(w) {
			return
		}
		if len(segments) >= 2 && segments[1] == "inbox" {
			h.handleInstanceInboxList(w, r, instanceID)
			return
		}
		if len(segments) >= 3 && segments[2] == "inbox" {
			h.handleInstanceAgentInbox(w, r, instanceID, segments[1:])
			return
		}
		if len(segments) >= 3 && segments[2] == "prompt-stack" {
			agentID, err := normalizeDashboardAgentID(segments[1])
			if err != nil {
				writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
				return
			}
			h.handlePromptStackAPI(w, r, instanceID, agentID, segments[3:])
			return
		}
		if len(segments) >= 3 && isInstanceContractAction(segments[2]) {
			agentID, err := normalizeDashboardAgentID(segments[1])
			if err != nil {
				writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
				return
			}
			h.handleAgentContractRoute(w, r, instanceID, agentID, segments[2:], true)
			return
		}
		h.handleInstanceAgents(w, r, instanceID, segments[1:])
	default:
		http.NotFound(w, r)
	}
}

func isInstanceContractAction(segment string) bool {
	switch strings.TrimSpace(segment) {
	case "resolved", "validate", "diff", "rollback-snapshot", "rollback-restore":
		return true
	default:
		return false
	}
}

func (h *Handler) handleInstanceAgentInbox(w http.ResponseWriter, r *http.Request, instanceID string, segments []string) {
	if len(segments) < 3 || segments[1] != "inbox" {
		http.NotFound(w, r)
		return
	}
	agentID, err := normalizeDashboardAgentID(segments[0])
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
		return
	}
	messageID := strings.TrimSpace(segments[2])
	if messageID == "" || strings.Contains(messageID, "/") {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_message_id", "invalid message id", nil)
		return
	}
	if len(segments) == 3 {
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.handleInstanceInboxMessage(w, instanceID, agentID, messageID)
		return
	}
	if len(segments) != 4 || r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	switch segments[3] {
	case "ack":
		h.ackInstanceInboxMessage(w, r, instanceID, agentID, messageID)
	case "run":
		h.runInstanceInboxMessage(w, r, instanceID, agentID, messageID)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleInstanceInboxList(w http.ResponseWriter, r *http.Request, instanceID string) {
	limit, offset, err := parseLimitOffset(r.URL.Query(), 20, 200)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "instances.invalid_pagination", err.Error(), nil)
		return
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentID != "" {
		agentID, err = normalizeDashboardAgentID(agentID)
		if err != nil {
			writeDashboardError(w, http.StatusBadRequest, "instances.invalid_agent_id", "invalid agent id", nil)
			return
		}
	}
	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_unavailable", "failed to open inbox store", nil)
		return
	}
	messages, total, err := h.listInstanceInboxMessages(store, instanceID, agentID, limit, offset)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{
		"instance_id": instanceID,
		"agent_id":    agentID,
		"messages":    messages,
		"count":       len(messages),
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func (h *Handler) handleInstanceInboxMessage(w http.ResponseWriter, instanceID, agentID, messageID string) {
	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_unavailable", "failed to open inbox store", nil)
		return
	}
	message, err := h.loadInstanceInboxMessage(store, instanceID, agentID, messageID)
	if err != nil {
		h.writeInboxLookupError(w, err)
		return
	}
	writeJSON(w, map[string]any{"instance_id": instanceID, "agent_id": agentID, "message": message})
}

func (h *Handler) ackInstanceInboxMessage(w http.ResponseWriter, r *http.Request, instanceID, agentID, messageID string) {
	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_unavailable", "failed to open inbox store", nil)
		return
	}
	message, session, envelope, err := h.lookupInboxMessageState(store, instanceID, agentID, messageID)
	if err != nil {
		h.writeInboxLookupError(w, err)
		return
	}
	note := "message acknowledged from dashboard inbox"
	if strings.EqualFold(message.Status, tools.AgentMessageStatusAcknowledged) {
		note = firstNonEmpty(strings.TrimSpace(message.Note), note)
	}
	if err := tools.AppendAgentMessageStatus(store, session.SessionID, tools.AgentMessageEnvelope{
		MessageID:       envelope.MessageID,
		Status:          tools.AgentMessageStatusAcknowledged,
		InstanceID:      envelope.InstanceID,
		FromAgentID:     envelope.FromAgentID,
		ToAgentID:       envelope.ToAgentID,
		Subject:         envelope.Subject,
		TaskID:          envelope.TaskID,
		SessionID:       envelope.SessionID,
		SourceSessionID: envelope.SourceSessionID,
		Channel:         envelope.Channel,
		UserID:          envelope.UserID,
		Message:         envelope.Message,
		RelatedRunID:    message.RelatedRunID,
		Note:            note,
		SentAt:          envelope.SentAt,
	}); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_ack_failed", err.Error(), nil)
		return
	}
	updated, err := h.loadInstanceInboxMessage(store, instanceID, agentID, messageID)
	if err != nil {
		h.writeInboxLookupError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "instance_id": instanceID, "agent_id": agentID, "message": updated})
}

func (h *Handler) runInstanceInboxMessage(w http.ResponseWriter, r *http.Request, instanceID, agentID, messageID string) {
	if h.agentRunner == nil {
		writeDashboardError(w, http.StatusNotImplemented, "instances.inbox_run_disabled", "dashboard agent runner is not configured", nil)
		return
	}
	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_unavailable", "failed to open inbox store", nil)
		return
	}
	message, session, envelope, err := h.lookupInboxMessageState(store, instanceID, agentID, messageID)
	if err != nil {
		h.writeInboxLookupError(w, err)
		return
	}
	if strings.TrimSpace(envelope.Message) == "" {
		writeDashboardError(w, http.StatusBadRequest, "instances.inbox_empty_message", "message has no runnable content", nil)
		return
	}
	var reqBody struct {
		ParentRunID       string   `json:"parent_run_id"`
		Source            string   `json:"source"`
		ThinkingMode      string   `json:"thinking_mode"`
		AllowedTools      []string `json:"allowed_tools"`
		MaxToolIterations int      `json:"max_tool_iterations"`
		TimeoutMS         int      `json:"timeout_ms"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) && !errors.Is(err, io.EOF) {
			writeDashboardError(w, http.StatusBadRequest, "instances.invalid_json", "invalid json body", nil)
			return
		}
	}
	workspaceRoot := filepath.Join(h.rootDir, "workspace")
	if instance, err := h.loadProjectedInstance(instanceID); err == nil && strings.TrimSpace(instance.Config.Workspace.Root) != "" {
		workspaceRoot = strings.TrimSpace(instance.Config.Workspace.Root)
	}
	toolReq := tools.Request{
		AgentID:    agentID,
		InstanceID: instanceID,
		Workspace:  workspaceRoot,
		Args: map[string]any{
			"parent_run_id":       strings.TrimSpace(reqBody.ParentRunID),
			"source":              firstNonEmpty(strings.TrimSpace(reqBody.Source), "dashboard/inbox"),
			"thinking_mode":       strings.TrimSpace(reqBody.ThinkingMode),
			"allowed_tools":       stringSliceToAny(reqBody.AllowedTools),
			"max_tool_iterations": reqBody.MaxToolIterations,
			"timeout_ms":          reqBody.TimeoutMS,
		},
	}
	result, runErr := tools.RunAgentMessageLifecycle(r.Context(), h.agentRunner, store, toolReq, tools.AgentMessageEnvelope{
		MessageID:       envelope.MessageID,
		Status:          message.Status,
		InstanceID:      envelope.InstanceID,
		FromAgentID:     envelope.FromAgentID,
		ToAgentID:       envelope.ToAgentID,
		Subject:         envelope.Subject,
		TaskID:          envelope.TaskID,
		SessionID:       envelope.SessionID,
		SourceSessionID: envelope.SourceSessionID,
		Channel:         envelope.Channel,
		UserID:          envelope.UserID,
		Message:         envelope.Message,
		SentAt:          envelope.SentAt,
	}, session.SessionID)
	updated, loadErr := h.loadInstanceInboxMessage(store, instanceID, agentID, messageID)
	if loadErr != nil {
		h.writeInboxLookupError(w, loadErr)
		return
	}
	statusCode := http.StatusOK
	if runErr != nil {
		statusCode = http.StatusBadGateway
	}
	writeJSONStatus(w, statusCode, map[string]any{
		"ok":          runErr == nil,
		"instance_id": instanceID,
		"agent_id":    agentID,
		"run_id":      result.RunID,
		"status":      result.Status,
		"message":     updated,
		"error":       errorString(runErr),
	})
}

func (h *Handler) listInstanceInboxMessages(store *chatstore.Store, instanceID, agentID string, limit, offset int) ([]dashboardInboxMessage, int, error) {
	agentIDs := []string{}
	if agentID != "" {
		agentIDs = append(agentIDs, agentID)
	} else {
		agents, err := instances.ListAgents(h.rootDir, instanceID)
		if err != nil {
			return nil, 0, err
		}
		for _, agent := range agents {
			agentIDs = append(agentIDs, agent.AgentID)
		}
	}
	sort.Strings(agentIDs)
	collected := make([]dashboardInboxMessage, 0)
	for _, candidateAgentID := range agentIDs {
		sessions, err := store.ListSessions(candidateAgentID, "", "", "agent-mail")
		if err != nil {
			return nil, 0, err
		}
		messageByID := map[string]dashboardInboxMessage{}
		for _, session := range sessions {
			recent, err := store.ReadRecentMessages(session.SessionID, 200)
			if err != nil {
				return nil, 0, err
			}
			for _, item := range recent {
				envelope := tools.DecodeAgentMessageEnvelope(item)
				message := dashboardInboxMessageFromStoreMessage(session.SessionID, item, envelope, candidateAgentID)
				if strings.TrimSpace(message.InstanceID) != instanceID {
					continue
				}
				if strings.TrimSpace(message.MessageID) == "" {
					continue
				}
				existing, ok := messageByID[message.MessageID]
				messageByID[message.MessageID] = mergeDashboardInboxMessages(existing, message, ok)
			}
		}
		for _, item := range messageByID {
			collected = append(collected, item)
		}
	}
	sort.Slice(collected, func(i, j int) bool {
		if collected[i].TS.Equal(collected[j].TS) {
			if collected[i].ToAgentID == collected[j].ToAgentID {
				return collected[i].MessageID > collected[j].MessageID
			}
			return collected[i].ToAgentID < collected[j].ToAgentID
		}
		return collected[i].TS.After(collected[j].TS)
	})
	total := len(collected)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return collected[offset:end], total, nil
}

func (h *Handler) lookupInboxMessageState(store *chatstore.Store, instanceID, agentID, messageID string) (dashboardInboxMessage, chatstore.Session, tools.AgentMessageEnvelope, error) {
	session, messages, err := findInboxMessageLifecycle(store, agentID, messageID)
	if err != nil {
		return dashboardInboxMessage{}, chatstore.Session{}, tools.AgentMessageEnvelope{}, err
	}
	message, envelope := mergeDashboardInboxMessageLifecycle(session.SessionID, messages, agentID)
	if strings.TrimSpace(message.InstanceID) != instanceID {
		return dashboardInboxMessage{}, chatstore.Session{}, tools.AgentMessageEnvelope{}, chatstore.ErrMessageNotFound
	}
	return message, session, envelope, nil
}

func (h *Handler) loadInstanceInboxMessage(store *chatstore.Store, instanceID, agentID, messageID string) (dashboardInboxMessage, error) {
	message, _, _, err := h.lookupInboxMessageState(store, instanceID, agentID, messageID)
	return message, err
}

func (h *Handler) writeInboxLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, chatstore.ErrMessageNotFound) {
		writeDashboardError(w, http.StatusNotFound, "instances.message_not_found", "inbox message not found", nil)
		return
	}
	writeDashboardError(w, http.StatusInternalServerError, "instances.inbox_failed", err.Error(), nil)
}

func dashboardInboxMessageFromStoreMessage(sessionID string, item chatstore.Message, envelope tools.AgentMessageEnvelope, fallbackAgentID string) dashboardInboxMessage {
	instanceID := firstNonEmpty(strings.TrimSpace(item.InstanceID), strings.TrimSpace(envelope.InstanceID))
	return dashboardInboxMessage{
		MessageID:       firstNonEmpty(strings.TrimSpace(item.MessageID), strings.TrimSpace(envelope.MessageID)),
		Status:          firstNonEmpty(strings.TrimSpace(item.Status), strings.TrimSpace(envelope.Status), tools.AgentMessageStatusQueued),
		InstanceID:      instanceID,
		SessionID:       sessionID,
		SourceSessionID: firstNonEmpty(strings.TrimSpace(item.SourceSessionID), strings.TrimSpace(envelope.SourceSessionID), strings.TrimSpace(envelope.SessionID)),
		FromAgentID:     firstNonEmpty(strings.TrimSpace(item.FromAgentID), strings.TrimSpace(envelope.FromAgentID)),
		ToAgentID:       firstNonEmpty(strings.TrimSpace(item.ToAgentID), strings.TrimSpace(envelope.ToAgentID), fallbackAgentID),
		Subject:         firstNonEmpty(strings.TrimSpace(item.Subject), strings.TrimSpace(envelope.Subject)),
		TaskID:          firstNonEmpty(strings.TrimSpace(item.TaskID), strings.TrimSpace(envelope.TaskID)),
		Channel:         firstNonEmpty(strings.TrimSpace(item.Channel), strings.TrimSpace(envelope.Channel)),
		UserID:          firstNonEmpty(strings.TrimSpace(item.UserID), strings.TrimSpace(envelope.UserID)),
		Message:         firstNonEmpty(strings.TrimSpace(envelope.Message)),
		RelatedRunID:    firstNonEmpty(strings.TrimSpace(item.RelatedRunID), strings.TrimSpace(envelope.RelatedRunID)),
		Note:            firstNonEmpty(strings.TrimSpace(item.Note), strings.TrimSpace(envelope.Note)),
		Error:           firstNonEmpty(strings.TrimSpace(item.Error), strings.TrimSpace(envelope.Error)),
		Role:            strings.TrimSpace(item.Role),
		TS:              item.TS.UTC(),
	}
}

func mergeDashboardInboxMessages(existing, next dashboardInboxMessage, hasExisting bool) dashboardInboxMessage {
	if !hasExisting {
		return next
	}
	merged := existing
	merged.MessageID = firstNonEmpty(next.MessageID, existing.MessageID)
	merged.InstanceID = firstNonEmpty(existing.InstanceID, next.InstanceID)
	merged.SessionID = firstNonEmpty(existing.SessionID, next.SessionID)
	merged.SourceSessionID = firstNonEmpty(existing.SourceSessionID, next.SourceSessionID)
	merged.FromAgentID = firstNonEmpty(existing.FromAgentID, next.FromAgentID)
	merged.ToAgentID = firstNonEmpty(existing.ToAgentID, next.ToAgentID)
	merged.Subject = firstNonEmpty(existing.Subject, next.Subject)
	merged.TaskID = firstNonEmpty(existing.TaskID, next.TaskID)
	merged.Channel = firstNonEmpty(existing.Channel, next.Channel)
	merged.UserID = firstNonEmpty(existing.UserID, next.UserID)
	merged.Message = firstNonEmpty(existing.Message, next.Message)
	merged.Role = firstNonEmpty(existing.Role, next.Role)
	if next.TS.After(existing.TS) {
		merged.Status = firstNonEmpty(next.Status, existing.Status)
		merged.RelatedRunID = firstNonEmpty(next.RelatedRunID, existing.RelatedRunID)
		merged.Note = firstNonEmpty(next.Note, existing.Note)
		merged.Error = firstNonEmpty(next.Error, existing.Error)
		merged.TS = next.TS
	} else {
		merged.Status = firstNonEmpty(existing.Status, next.Status)
		merged.RelatedRunID = firstNonEmpty(existing.RelatedRunID, next.RelatedRunID)
		merged.Note = firstNonEmpty(existing.Note, next.Note)
		merged.Error = firstNonEmpty(existing.Error, next.Error)
	}
	return merged
}

func mergeDashboardInboxMessageLifecycle(sessionID string, items []chatstore.Message, fallbackAgentID string) (dashboardInboxMessage, tools.AgentMessageEnvelope) {
	var merged dashboardInboxMessage
	var envelope tools.AgentMessageEnvelope
	hasMessage := false
	for _, item := range items {
		decoded := tools.DecodeAgentMessageEnvelope(item)
		message := dashboardInboxMessageFromStoreMessage(sessionID, item, decoded, fallbackAgentID)
		merged = mergeDashboardInboxMessages(merged, message, hasMessage)
		if !hasMessage {
			envelope = decoded
			hasMessage = true
			continue
		}
		envelope.MessageID = firstNonEmpty(envelope.MessageID, decoded.MessageID)
		envelope.InstanceID = firstNonEmpty(envelope.InstanceID, decoded.InstanceID)
		envelope.FromAgentID = firstNonEmpty(envelope.FromAgentID, decoded.FromAgentID)
		envelope.ToAgentID = firstNonEmpty(envelope.ToAgentID, decoded.ToAgentID)
		envelope.Subject = firstNonEmpty(envelope.Subject, decoded.Subject)
		envelope.TaskID = firstNonEmpty(envelope.TaskID, decoded.TaskID)
		envelope.SessionID = firstNonEmpty(envelope.SessionID, decoded.SessionID)
		envelope.SourceSessionID = firstNonEmpty(envelope.SourceSessionID, decoded.SourceSessionID)
		envelope.Channel = firstNonEmpty(envelope.Channel, decoded.Channel)
		envelope.UserID = firstNonEmpty(envelope.UserID, decoded.UserID)
		envelope.Message = firstNonEmpty(envelope.Message, decoded.Message)
		if message.TS.Equal(merged.TS) {
			envelope.Status = firstNonEmpty(decoded.Status, envelope.Status)
			envelope.RelatedRunID = firstNonEmpty(decoded.RelatedRunID, envelope.RelatedRunID)
			envelope.Note = firstNonEmpty(decoded.Note, envelope.Note)
			envelope.Error = firstNonEmpty(decoded.Error, envelope.Error)
		}
	}
	envelope.Status = firstNonEmpty(merged.Status, envelope.Status)
	envelope.RelatedRunID = firstNonEmpty(merged.RelatedRunID, envelope.RelatedRunID)
	envelope.Note = firstNonEmpty(merged.Note, envelope.Note)
	envelope.Error = firstNonEmpty(merged.Error, envelope.Error)
	return merged, envelope
}

func findInboxMessageLifecycle(store *chatstore.Store, agentID, messageID string) (chatstore.Session, []chatstore.Message, error) {
	sessions, err := store.ListSessions(agentID, "", "", "agent-mail")
	if err != nil {
		return chatstore.Session{}, nil, err
	}
	for _, session := range sessions {
		recent, err := store.ReadRecentMessages(session.SessionID, chatstore.DefaultMaxHistoryCount)
		if err != nil {
			return chatstore.Session{}, nil, err
		}
		matches := make([]chatstore.Message, 0, len(recent))
		for _, item := range recent {
			if strings.TrimSpace(item.MessageID) == messageID || strings.TrimSpace(tools.DecodeAgentMessageEnvelope(item).MessageID) == messageID {
				matches = append(matches, item)
			}
		}
		if len(matches) > 0 {
			return session, matches, nil
		}
	}
	return chatstore.Session{}, nil, chatstore.ErrMessageNotFound
}

func stringSliceToAny(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
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
	if !h.requireWizardFeature(w) {
		return
	}
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
	if !h.requireWizardFeature(w) {
		return
	}
	if !h.requireInstanceControlFeature(w) {
		return
	}
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
	if !h.requireWizardFeature(w) {
		return
	}
	if !h.requireInstanceControlFeature(w) {
		return
	}
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
	if !h.requireWizardFeature(w) {
		return
	}
	if !h.requireInstanceAgentsFeature(w) {
		return
	}
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
	if !h.requireWizardFeature(w) {
		return
	}
	if !h.requireInstanceAgentsFeature(w) {
		return
	}
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
	if _, err := instances.LoadAgentManifest(h.rootDir, instance.ID, plan.AgentID); err == nil {
		writeDashboardError(w, http.StatusConflict, "instances.agent_exists", "agent already exists", nil)
		return
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeDashboardError(w, http.StatusInternalServerError, "instances.load_failed", err.Error(), nil)
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
