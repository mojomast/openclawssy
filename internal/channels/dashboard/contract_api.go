package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/config"
	agentcontract "openclawssy/internal/contract"
)

const maxRollbackSnapshotsPerAgent = 10

type agentRollbackSnapshot struct {
	ID        string
	CreatedAt time.Time
	Config    config.Config
}

type contractFieldDiff struct {
	Field        string `json:"field"`
	TargetValue  any    `json:"target_value"`
	BaseValue    any    `json:"base_value"`
	TargetSource string `json:"target_source"`
	BaseSource   string `json:"base_source"`
}

func (h *Handler) handleAgentContractAPI(w http.ResponseWriter, r *http.Request) {
	rawAgentID, action, ok := parseAgentContractRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	agentID, err := normalizeDashboardAgentID(rawAgentID)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "agents.invalid_agent_id", "invalid agent id", nil)
		return
	}
	if !h.dashboardAgentExists(agentID) {
		writeDashboardError(w, http.StatusNotFound, "agents.not_found", "agent not found", nil)
		return
	}

	switch action {
	case "resolved":
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.getAgentResolvedContract(w, agentID)
	case "validate":
		if r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.validateAgentContract(w, r, agentID)
	case "diff":
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.getAgentContractDiff(w, r, agentID)
	case "rollback-snapshot":
		if r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.createAgentRollbackSnapshot(w, agentID)
	case "rollback-restore":
		if r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.restoreAgentRollbackSnapshot(w, r, agentID)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) createAgentRollbackSnapshot(w http.ResponseWriter, agentID string) {
	cfg, err := h.loadDashboardConfig()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "contract.snapshot_failed", "failed to encode config snapshot", nil)
		return
	}
	var cloned config.Config
	if err := json.Unmarshal(raw, &cloned); err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "contract.snapshot_failed", "failed to decode config snapshot", nil)
		return
	}

	now := time.Now().UTC()
	snapshot := agentRollbackSnapshot{
		ID:        fmt.Sprintf("%d", now.UnixNano()),
		CreatedAt: now,
		Config:    cloned,
	}

	h.rollbackMu.Lock()
	existing := append([]agentRollbackSnapshot{snapshot}, h.rollbackByAgent[agentID]...)
	if len(existing) > maxRollbackSnapshotsPerAgent {
		existing = existing[:maxRollbackSnapshotsPerAgent]
	}
	h.rollbackByAgent[agentID] = existing
	h.rollbackMu.Unlock()

	writeJSON(w, map[string]any{
		"ok": true,
		"snapshot": map[string]any{
			"id":         snapshot.ID,
			"created_at": snapshot.CreatedAt.Format(time.RFC3339Nano),
		},
	})
}

func (h *Handler) restoreAgentRollbackSnapshot(w http.ResponseWriter, r *http.Request, agentID string) {
	var req struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "contract.invalid_json", "invalid json body", nil)
		return
	}
	snapshotID := strings.TrimSpace(req.SnapshotID)
	if snapshotID == "" {
		writeDashboardError(w, http.StatusBadRequest, "contract.missing_snapshot_id", "snapshot_id is required", nil)
		return
	}

	h.rollbackMu.Lock()
	snapshots := h.rollbackByAgent[agentID]
	var match agentRollbackSnapshot
	found := false
	for _, snapshot := range snapshots {
		if snapshot.ID == snapshotID {
			match = snapshot
			found = true
			break
		}
	}
	h.rollbackMu.Unlock()

	if !found {
		writeDashboardError(w, http.StatusNotFound, "contract.snapshot_not_found", "snapshot not found", nil)
		return
	}

	if err := saveDashboardConfig(filepath.Join(h.rootDir, ".openclawssy", "config.json"), match.Config); err != nil {
		writeConfigValidationError(w, err, match.Config)
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}

func parseAgentContractRoute(requestPath string) (agentID string, action string, ok bool) {
	suffix := strings.TrimPrefix(requestPath, "/api/admin/agents/")
	if suffix == requestPath {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (h *Handler) dashboardAgentExists(agentID string) bool {
	for _, id := range h.listDashboardAgentIDs() {
		if id == agentID {
			return true
		}
	}
	return false
}

func (h *Handler) loadDashboardConfig() (config.Config, error) {
	return config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
}

func (h *Handler) getAgentResolvedContract(w http.ResponseWriter, agentID string) {
	cfg, err := h.loadDashboardConfig()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}
	resolved, err := agentcontract.Resolve(cfg, agentID, nil)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "contract.resolve_failed", err.Error(), nil)
		return
	}
	writeJSON(w, resolved)
}

func (h *Handler) validateAgentContract(w http.ResponseWriter, r *http.Request, agentID string) {
	cfg, err := h.loadDashboardConfig()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}
	resolved, err := agentcontract.Resolve(cfg, agentID, nil)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "contract.resolve_failed", err.Error(), nil)
		return
	}

	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "contract.invalid_json", "invalid json body", nil)
		return
	}
	proposed, err := applyContractPatch(resolved, patch, agentID)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "contract.invalid_patch", err.Error(), nil)
		return
	}

	if err := proposed.Validate(); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"ok":           false,
			"error":        err.Error(),
			"field_errors": collectContractFieldErrors(err),
		})
		return
	}

	writeJSON(w, map[string]any{"ok": true, "field_errors": map[string]string{}})
}

func applyContractPatch(base agentcontract.AgentContract, patch map[string]any, agentID string) (agentcontract.AgentContract, error) {
	if patch == nil {
		patch = map[string]any{}
	}
	baselineMap, err := contractToMap(base)
	if err != nil {
		return agentcontract.AgentContract{}, fmt.Errorf("encode baseline contract: %w", err)
	}
	baselineMap = mergeJSONObjects(baselineMap, patch)

	if identityRaw, ok := patch["identity"].(map[string]any); ok {
		if rawID, hasID := identityRaw["agent_id"]; hasID {
			requested := strings.TrimSpace(fmt.Sprint(rawID))
			if requested != "" && requested != agentID {
				return agentcontract.AgentContract{}, fmt.Errorf("identity.agent_id must match requested agent id %q", agentID)
			}
		}
	}

	raw, err := json.Marshal(baselineMap)
	if err != nil {
		return agentcontract.AgentContract{}, fmt.Errorf("encode merged contract: %w", err)
	}
	var proposed agentcontract.AgentContract
	if err := json.Unmarshal(raw, &proposed); err != nil {
		return agentcontract.AgentContract{}, fmt.Errorf("decode merged contract: %w", err)
	}

	proposed.Identity.AgentID = agentID
	if strings.TrimSpace(proposed.Identity.DisplayName) == "" {
		proposed.Identity.DisplayName = agentID
	}

	return proposed, nil
}

func (h *Handler) getAgentContractDiff(w http.ResponseWriter, r *http.Request, agentID string) {
	cfg, err := h.loadDashboardConfig()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}

	target, err := agentcontract.Resolve(cfg, agentID, nil)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "contract.resolve_failed", err.Error(), nil)
		return
	}

	base := strings.TrimSpace(r.URL.Query().Get("base"))
	if base == "" {
		base = agentcontract.InheritanceSourceGlobal
	}

	var (
		baseLabel    string
		baseContract agentcontract.AgentContract
	)

	if strings.EqualFold(base, agentcontract.InheritanceSourceGlobal) {
		baseLabel = agentcontract.InheritanceSourceGlobal
		globalCfg := cloneConfigForGlobalDiff(cfg, agentID)
		baseContract, err = agentcontract.Resolve(globalCfg, agentID, nil)
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "contract.resolve_failed", err.Error(), nil)
			return
		}
	} else {
		otherID, err := normalizeDashboardAgentID(base)
		if err != nil {
			writeDashboardError(w, http.StatusBadRequest, "agents.invalid_base", "invalid base agent", nil)
			return
		}
		if !h.dashboardAgentExists(otherID) {
			writeDashboardError(w, http.StatusNotFound, "agents.base_not_found", "base agent not found", nil)
			return
		}
		baseLabel = otherID
		baseContract, err = agentcontract.Resolve(cfg, otherID, nil)
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "contract.resolve_failed", err.Error(), nil)
			return
		}
	}

	differences, err := diffAgentContracts(target, baseContract)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "contract.diff_failed", err.Error(), nil)
		return
	}

	writeJSON(w, map[string]any{
		"agent_id":      agentID,
		"base":          baseLabel,
		"differences":   differences,
		"count":         len(differences),
		"resolved":      target,
		"base_contract": baseContract,
	})
}

func cloneConfigForGlobalDiff(cfg config.Config, excludedAgentID string) config.Config {
	cloned := cfg
	cloned.Agents.Profiles = make(map[string]config.AgentProfile, len(cfg.Agents.Profiles))
	for agentID, profile := range cfg.Agents.Profiles {
		if agentID == excludedAgentID {
			continue
		}
		cloned.Agents.Profiles[agentID] = profile
	}
	cloned.Agents.SubAgentOverrides = make(map[string]config.SubAgentRestrictions, len(cfg.Agents.SubAgentOverrides))
	for agentID, restrictions := range cfg.Agents.SubAgentOverrides {
		if agentID == excludedAgentID {
			continue
		}
		cloned.Agents.SubAgentOverrides[agentID] = restrictions
	}
	return cloned
}

func diffAgentContracts(target, base agentcontract.AgentContract) ([]contractFieldDiff, error) {
	targetFields, err := flattenContractFields(target)
	if err != nil {
		return nil, err
	}
	baseFields, err := flattenContractFields(base)
	if err != nil {
		return nil, err
	}

	fieldSet := make(map[string]struct{}, len(targetFields)+len(baseFields))
	for key := range targetFields {
		fieldSet[key] = struct{}{}
	}
	for key := range baseFields {
		fieldSet[key] = struct{}{}
	}

	fields := make([]string, 0, len(fieldSet))
	for key := range fieldSet {
		fields = append(fields, key)
	}
	sort.Strings(fields)

	diffs := make([]contractFieldDiff, 0)
	for _, field := range fields {
		targetValue, hasTarget := targetFields[field]
		if !hasTarget {
			targetValue = nil
		}
		baseValue, hasBase := baseFields[field]
		if !hasBase {
			baseValue = nil
		}
		if reflect.DeepEqual(targetValue, baseValue) {
			continue
		}
		diffs = append(diffs, contractFieldDiff{
			Field:        field,
			TargetValue:  targetValue,
			BaseValue:    baseValue,
			TargetSource: inheritanceSourceForField(target, field),
			BaseSource:   inheritanceSourceForField(base, field),
		})
	}

	return diffs, nil
}

func flattenContractFields(value agentcontract.AgentContract) (map[string]any, error) {
	m, err := contractToMap(value)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any)
	flattenJSONValue("", m, out)
	return out, nil
}

func contractToMap(value agentcontract.AgentContract) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func flattenJSONValue(prefix string, value any, out map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			if next == "inheritance" || strings.HasPrefix(next, "inheritance.") {
				continue
			}
			flattenJSONValue(next, typed[key], out)
		}
	case []any:
		if prefix != "" {
			out[prefix] = typed
		}
	default:
		if prefix != "" {
			out[prefix] = typed
		}
	}
}

func inheritanceSourceForField(value agentcontract.AgentContract, field string) string {
	if source, ok := value.Inheritance.Source[field]; ok && strings.TrimSpace(source) != "" {
		return source
	}
	section, _, hasDot := strings.Cut(field, ".")
	if hasDot {
		if source, ok := value.Inheritance.Source[section]; ok && strings.TrimSpace(source) != "" {
			return source
		}
	}
	return agentcontract.InheritanceSourceGlobal
}

func collectContractFieldErrors(err error) map[string]string {
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)

	field := "contract"
	switch {
	case strings.Contains(lower, "identity.agent_id") || strings.Contains(lower, "identity agent id") || strings.Contains(lower, "agent_id"):
		field = "identity.agent_id"
	case strings.Contains(lower, "delegation mode") || strings.Contains(lower, "delegation_policy.mode"):
		field = "delegation_policy.mode"
	case strings.Contains(lower, "delegation_policy.threshold"):
		field = "delegation_policy.threshold"
	case strings.Contains(lower, "delegation_policy.cooldown"):
		field = "delegation_policy.cooldown"
	case strings.Contains(lower, "delegation_policy.max_depth"):
		field = "delegation_policy.max_depth"
	case strings.Contains(lower, "memory_policy.max_working_items"):
		field = "memory_policy.max_working_items"
	case strings.Contains(lower, "memory_policy.max_prompt_tokens"):
		field = "memory_policy.max_prompt_tokens"
	case strings.Contains(lower, "model_policy.max_tokens"):
		field = "model_policy.max_tokens"
	case strings.Contains(lower, "model_policy.timeout_ms"):
		field = "model_policy.timeout_ms"
	case strings.Contains(lower, "sandbox_policy.docker.cpu_limit"):
		field = "sandbox_policy.docker.cpu_limit"
	case strings.Contains(lower, "sandbox_policy.docker.memory_limit_mb"):
		field = "sandbox_policy.docker.memory_limit_mb"
	case strings.Contains(lower, "inheritance.source"):
		field = "inheritance.source"
	}

	return map[string]string{field: message}
}

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
