package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"openclawssy/internal/config"
	"openclawssy/internal/roles"
)

func (h *Handler) handleRoles(w http.ResponseWriter, r *http.Request) {
	if !h.requireInstanceAgentsFeature(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.listRoleTemplates(w)
	case http.MethodPost:
		h.createRoleTemplate(w, r)
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) handleRoleByName(w http.ResponseWriter, r *http.Request) {
	if !h.requireInstanceAgentsFeature(w) {
		return
	}
	name, err := parseRoleTemplateNameFromPath(r.URL.Path)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "roles.invalid_name", err.Error(), nil)
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.updateRoleTemplate(w, r, name)
	case http.MethodDelete:
		h.deleteRoleTemplate(w, name)
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) listRoleTemplates(w http.ResponseWriter) {
	store, _, err := h.roleTemplateStore()
	if err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	all := store.List()
	writeJSON(w, map[string]any{
		"roles": all,
		"count": len(all),
	})
}

func (h *Handler) createRoleTemplate(w http.ResponseWriter, r *http.Request) {
	var req roles.RoleTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "roles.invalid_json", "invalid json body", nil)
		return
	}

	store, cfg, err := h.roleTemplateStore()
	if err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	if err := store.CreateCustom(req); err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	if err := h.persistCustomRoleTemplates(cfg, store); err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	created, _ := store.Get(req.Name)
	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"ok":   true,
		"role": created,
	})
}

func (h *Handler) updateRoleTemplate(w http.ResponseWriter, r *http.Request, name string) {
	var req roles.RoleTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "roles.invalid_json", "invalid json body", nil)
		return
	}

	bodyName := strings.TrimSpace(req.Name)
	if bodyName != "" && !strings.EqualFold(bodyName, name) {
		writeDashboardError(w, http.StatusBadRequest, "roles.name_mismatch", "body name must match path name", nil)
		return
	}
	req.Name = name

	store, cfg, err := h.roleTemplateStore()
	if err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	if err := store.UpdateCustom(req); err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	if err := h.persistCustomRoleTemplates(cfg, store); err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	updated, _ := store.Get(name)
	writeJSON(w, map[string]any{
		"ok":   true,
		"role": updated,
	})
}

func (h *Handler) deleteRoleTemplate(w http.ResponseWriter, name string) {
	store, cfg, err := h.roleTemplateStore()
	if err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	if err := store.Delete(name); err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	if err := h.persistCustomRoleTemplates(cfg, store); err != nil {
		h.writeRoleTemplateError(w, err)
		return
	}

	writeJSON(w, map[string]any{
		"ok":      true,
		"deleted": strings.ToLower(name),
	})
}

func (h *Handler) roleTemplateStore() (*roles.RoleStore, configWithPath, error) {
	cfg, err := h.loadDashboardConfig()
	if err != nil {
		return nil, configWithPath{}, err
	}

	store, err := roles.NewRoleStore(cfg.Agents.CustomRoleTemplates)
	if err != nil {
		return nil, configWithPath{}, err
	}

	return store, configWithPath{config: cfg, path: filepath.Join(h.rootDir, ".openclawssy", "config.json")}, nil
}

type configWithPath struct {
	config config.Config
	path   string
}

func (h *Handler) persistCustomRoleTemplates(state configWithPath, store *roles.RoleStore) error {
	next := state.config
	next.Agents.CustomRoleTemplates = store.Custom()
	return saveDashboardConfig(state.path, next)
}

func parseRoleTemplateNameFromPath(path string) (string, error) {
	suffix := strings.TrimPrefix(path, "/api/admin/roles/")
	if suffix == path {
		return "", errors.New("invalid role name")
	}
	if suffix == "" || strings.Contains(suffix, "/") {
		return "", errors.New("invalid role name")
	}
	decoded, err := url.PathUnescape(suffix)
	if err != nil {
		return "", errors.New("invalid role name")
	}
	name := strings.TrimSpace(decoded)
	if name == "" {
		return "", errors.New("invalid role name")
	}
	return strings.ToLower(name), nil
}

func (h *Handler) writeRoleTemplateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, roles.ErrDuplicateRoleName):
		writeDashboardError(w, http.StatusConflict, "roles.duplicate_name", err.Error(), nil)
	case errors.Is(err, roles.ErrBuiltInRoleImmutable):
		writeDashboardError(w, http.StatusForbidden, "roles.builtin_immutable", err.Error(), nil)
	case errors.Is(err, roles.ErrRoleNotFound):
		writeDashboardError(w, http.StatusNotFound, "roles.not_found", err.Error(), nil)
	case strings.Contains(strings.ToLower(err.Error()), "invalid custom template"):
		writeDashboardError(w, http.StatusBadRequest, "roles.invalid_template", err.Error(), nil)
	case strings.Contains(strings.ToLower(err.Error()), "role name") ||
		strings.Contains(strings.ToLower(err.Error()), "allowed_tools") ||
		strings.Contains(strings.ToLower(err.Error()), "timeout_ms"):
		writeDashboardError(w, http.StatusBadRequest, "roles.invalid_template", err.Error(), nil)
	case strings.Contains(strings.ToLower(err.Error()), "config"):
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
	default:
		writeDashboardError(w, http.StatusInternalServerError, "roles.operation_failed", err.Error(), nil)
	}
}
