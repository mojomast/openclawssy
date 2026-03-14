package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/config"
	"openclawssy/internal/roles"
)

func TestRolesListIncludesBuiltInAndCustomTemplates(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Agents.CustomRoleTemplates = []roles.RoleTemplate{
		{
			Name:              "analyst",
			Description:       "Custom analysis role",
			AllowedTools:      []string{"fs.read", "fs.search"},
			MaxIterations:     12,
			TimeoutMS:         90000,
			MemoryAccessScope: "read_only",
		},
	}
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/admin/roles", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		Roles []roles.RoleTemplate `json:"roles"`
		Count int                  `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != len(payload.Roles) {
		t.Fatalf("expected count %d to match role length %d", payload.Count, len(payload.Roles))
	}
	if len(payload.Roles) != 7 {
		t.Fatalf("expected 7 roles (6 built-in + 1 custom), got %d", len(payload.Roles))
	}

	var (
		builtIns int
		found    bool
	)
	for _, role := range payload.Roles {
		if role.IsBuiltin {
			builtIns++
		}
		if role.Name == "analyst" {
			found = true
			if role.IsBuiltin {
				t.Fatalf("expected analyst to be custom (is_builtin=false)")
			}
		}
	}
	if builtIns != 6 {
		t.Fatalf("expected 6 built-in roles, got %d", builtIns)
	}
	if !found {
		t.Fatalf("expected analyst custom role in list")
	}
}

func TestRolesCreateUpdateDeleteCustomTemplate(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	createBody := `{
		"name":"analyst",
		"description":"Initial role",
		"allowed_tools":["fs.read","fs.search"],
		"max_iterations":12,
		"timeout_ms":90000,
		"memory_access_scope":"read_only",
		"writable_paths":["workspace/docs/**"],
		"prompt_contract":"Summarize findings with evidence.",
		"output_schema":{"type":"object"},
		"handoff_schema":{"type":"object"},
		"escalation_rules":["Escalate when source docs conflict."],
		"delegation_permissions":["scout"]
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/roles", bytes.NewBufferString(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	mux.ServeHTTP(createRR, createReq)

	if createRR.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d (%s)", http.StatusCreated, createRR.Code, createRR.Body.String())
	}

	updateBody := `{
		"name":"analyst",
		"description":"Updated role",
		"allowed_tools":["fs.read"],
		"max_iterations":8,
		"timeout_ms":45000,
		"memory_access_scope":"summary",
		"writable_paths":["workspace/reports/**"],
		"prompt_contract":"Updated contract",
		"output_schema":{"type":"object","required":["summary"]},
		"handoff_schema":{"type":"object","required":["status"]},
		"escalation_rules":["Escalate on missing evidence."],
		"delegation_permissions":["reviewer"]
	}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/roles/analyst", bytes.NewBufferString(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRR := httptest.NewRecorder()
	mux.ServeHTTP(updateRR, updateReq)

	if updateRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, updateRR.Code, updateRR.Body.String())
	}

	getRR := httptest.NewRecorder()
	mux.ServeHTTP(getRR, httptest.NewRequest(http.MethodGet, "/api/admin/roles", nil))
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, getRR.Code, getRR.Body.String())
	}
	var listPayload struct {
		Roles []roles.RoleTemplate `json:"roles"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	var analyst roles.RoleTemplate
	found := false
	for _, template := range listPayload.Roles {
		if template.Name == "analyst" {
			analyst = template
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected analyst template in role list")
	}
	if analyst.Description != "Updated role" {
		t.Fatalf("expected updated role description, got %q", analyst.Description)
	}
	if analyst.TimeoutMS != 45000 {
		t.Fatalf("expected timeout_ms 45000, got %d", analyst.TimeoutMS)
	}

	deleteRR := httptest.NewRecorder()
	mux.ServeHTTP(deleteRR, httptest.NewRequest(http.MethodDelete, "/api/admin/roles/analyst", nil))
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, deleteRR.Code, deleteRR.Body.String())
	}

	configAfterDelete, err := config.LoadOrDefault(filepath.Join(root, ".openclawssy", "config.json"))
	if err != nil {
		t.Fatalf("load config after delete: %v", err)
	}
	if len(configAfterDelete.Agents.CustomRoleTemplates) != 0 {
		t.Fatalf("expected no custom role templates persisted after delete, got %d", len(configAfterDelete.Agents.CustomRoleTemplates))
	}
}

func TestRolesDeleteBuiltInRejected(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/admin/roles/scout", nil))

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d (%s)", http.StatusForbidden, rr.Code, rr.Body.String())
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "roles.builtin_immutable" {
		t.Fatalf("expected error code roles.builtin_immutable, got %q", payload.Error.Code)
	}
}
