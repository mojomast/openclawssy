package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/config"
	agentcontract "openclawssy/internal/contract"
	"openclawssy/internal/roles"
)

func TestContractResolvedEndpointReturnsResolvedContract(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Profiles["alpha"] = config.AgentProfile{
		Model: config.ModelConfig{
			Provider:  "openrouter",
			Name:      "moonshot/test",
			TimeoutMS: 45000,
		},
	}
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents/alpha/resolved", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload agentcontract.AgentContract
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("resolved contract must validate: %v", err)
	}
	if payload.Identity.AgentID != "alpha" {
		t.Fatalf("expected identity.agent_id alpha, got %q", payload.Identity.AgentID)
	}
	if payload.ModelPolicy.Provider != "openrouter" {
		t.Fatalf("expected model_policy.provider openrouter, got %q", payload.ModelPolicy.Provider)
	}
	if payload.Inheritance.Source["model_policy.provider"] != agentcontract.InheritanceSourceAgentProfile {
		t.Fatalf("expected model_policy.provider source %q, got %q", agentcontract.InheritanceSourceAgentProfile, payload.Inheritance.Source["model_policy.provider"])
	}
}

func TestIntegrationResolvedContractIncludesPromptStackAndRoleReferences(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Profiles["alpha"] = config.AgentProfile{
		Model: config.ModelConfig{Provider: "openrouter", Name: "moonshot/test"},
	}
	cfg.Agents.CustomRoleTemplates = []roles.RoleTemplate{
		{
			Name:          "db_admin",
			Description:   "Database migration specialist",
			AllowedTools:  []string{"fs.read", "shell.exec"},
			MaxIterations: 8,
			TimeoutMS:     45000,
		},
	}
	cfg.Agents.SubAgentOverrides = map[string]config.SubAgentRestrictions{
		"alpha": {
			AllowedTools:      []string{"fs.read", "web.get"},
			MaxToolIterations: 6,
			TimeoutMS:         20000,
			ThinkingMode:      "always",
			DelegationMode:    "suggest_only",
		},
	}
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	stackText := "Prompt stack integration marker"
	updateReq := httptest.NewRequest(
		http.MethodPut,
		"/api/admin/agents/alpha/prompt-stack/agent_identity",
		bytes.NewBufferString(`{"content":"`+stackText+`"}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRR := httptest.NewRecorder()
	mux.ServeHTTP(updateRR, updateReq)
	if updateRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, updateRR.Code, updateRR.Body.String())
	}

	payload := getResolvedContractForAgent(t, mux, "alpha")
	if !strings.Contains(payload.SystemPrompt.Content, stackText) {
		t.Fatalf("expected system_prompt.content to include prompt stack text %q, got %q", stackText, payload.SystemPrompt.Content)
	}
	if payload.SystemPrompt.Source != "prompt_stack" {
		t.Fatalf("expected system_prompt.source prompt_stack, got %q", payload.SystemPrompt.Source)
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("resolved contract must validate: %v", err)
	}

	foundCustomRole := false
	for _, template := range payload.DelegationPolicy.RoleTemplates {
		if template.Name == "db_admin" {
			foundCustomRole = true
			if template.IsBuiltIn {
				t.Fatalf("expected custom role template to be marked as non built-in")
			}
		}
	}
	if !foundCustomRole {
		t.Fatalf("expected delegation_policy.role_templates to include custom role db_admin")
	}

	if got := payload.DelegationPolicy.RoleOverrides["max_tool_iterations"]; got != float64(6) {
		t.Fatalf("expected max_tool_iterations role override 6, got %#v", got)
	}
	if got := payload.DelegationPolicy.RoleOverrides["delegation_mode"]; got != "suggest_only" {
		t.Fatalf("expected delegation_mode override suggest_only, got %#v", got)
	}
	if payload.Inheritance.Source["delegation_policy.role_templates"] != agentcontract.InheritanceSourceGlobal {
		t.Fatalf("expected role template source global, got %q", payload.Inheritance.Source["delegation_policy.role_templates"])
	}
	if payload.Inheritance.Source["delegation_policy.role_overrides"] != agentcontract.InheritanceSourceSubagentOverride {
		t.Fatalf("expected role override source subagent-override, got %q", payload.Inheritance.Source["delegation_policy.role_overrides"])
	}
}

func TestIntegrationResolvedContractReloadsAfterConfigChange(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Profiles["alpha"] = config.AgentProfile{
		Model: config.ModelConfig{Provider: "openrouter", Name: "moonshot/test"},
	}
	cfg.Agents.CustomRoleTemplates = []roles.RoleTemplate{{
		Name:          "db_admin",
		Description:   "Database migration specialist",
		AllowedTools:  []string{"fs.read"},
		MaxIterations: 4,
		TimeoutMS:     15000,
	}}
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	first := getResolvedContractForAgent(t, mux, "alpha")
	if first.ModelPolicy.Provider != "openrouter" {
		t.Fatalf("expected first provider openrouter, got %q", first.ModelPolicy.Provider)
	}
	if !contractHasRoleTemplate(first, "db_admin") {
		t.Fatalf("expected first contract role templates to include db_admin")
	}

	mutated := cfg
	mutated.Agents.Profiles["alpha"] = config.AgentProfile{
		Model: config.ModelConfig{Provider: "openai", Name: "gpt-5"},
	}
	mutated.Agents.CustomRoleTemplates = []roles.RoleTemplate{{
		Name:          "qa_bot",
		Description:   "Regression verifier",
		AllowedTools:  []string{"fs.read", "test.run"},
		MaxIterations: 7,
		TimeoutMS:     22000,
	}}
	writeContractDashboardConfig(t, root, mutated)

	second := getResolvedContractForAgent(t, mux, "alpha")
	if second.ModelPolicy.Provider != "openai" {
		t.Fatalf("expected second provider openai after config change, got %q", second.ModelPolicy.Provider)
	}
	if contractHasRoleTemplate(second, "db_admin") {
		t.Fatalf("did not expect stale role template db_admin after config update")
	}
	if !contractHasRoleTemplate(second, "qa_bot") {
		t.Fatalf("expected updated role template qa_bot after config update")
	}
}

func TestContractResolvedEndpointUnknownAgentNotFound(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents/nonexistent/resolved", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d (%s)", http.StatusNotFound, rr.Code, rr.Body.String())
	}
	if got := contractErrorCode(t, rr.Body.Bytes()); got != "agents.not_found" {
		t.Fatalf("expected error code agents.not_found, got %q", got)
	}
}

func TestContractValidateEndpointReturnsFieldErrors(t *testing.T) {
	root := t.TempDir()
	writeContractDashboardConfig(t, root, config.Default())

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	badPatch := []byte(`{"model_policy":{"timeout_ms":-1}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/validate", bytes.NewReader(badPatch))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d (%s)", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	var payload struct {
		OK          bool              `json:"ok"`
		FieldErrors map[string]string `json:"field_errors"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.OK {
		t.Fatalf("expected ok=false for invalid contract patch")
	}
	if payload.FieldErrors["model_policy.timeout_ms"] == "" {
		t.Fatalf("expected field_errors.model_policy.timeout_ms, got %#v", payload.FieldErrors)
	}
}

func TestContractDiffEndpointGlobalIncludesFieldLevelChanges(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Profiles["alpha"] = config.AgentProfile{
		Model: config.ModelConfig{Provider: "openrouter"},
	}
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents/alpha/diff?base=global", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		AgentID     string                     `json:"agent_id"`
		Base        string                     `json:"base"`
		Differences []contractDiffFieldPayload `json:"differences"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.AgentID != "alpha" || payload.Base != "global" {
		t.Fatalf("unexpected diff header: %+v", payload)
	}

	change, ok := contractDiffByField(payload.Differences, "model_policy.provider")
	if !ok {
		t.Fatalf("expected model_policy.provider diff, got %#v", payload.Differences)
	}
	if change.TargetValue != "openrouter" {
		t.Fatalf("expected target value openrouter, got %#v", change.TargetValue)
	}
	if change.TargetSource != agentcontract.InheritanceSourceAgentProfile {
		t.Fatalf("expected target source %q, got %q", agentcontract.InheritanceSourceAgentProfile, change.TargetSource)
	}
	if change.BaseSource != agentcontract.InheritanceSourceGlobal {
		t.Fatalf("expected base source %q, got %q", agentcontract.InheritanceSourceGlobal, change.BaseSource)
	}
}

func TestContractDiffEndpointSupportsOtherAgentBase(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Agents.Profiles["alpha"] = config.AgentProfile{Model: config.ModelConfig{Provider: "openai"}}
	cfg.Agents.Profiles["beta"] = config.AgentProfile{Model: config.ModelConfig{Provider: "openrouter"}}
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents/alpha/diff?base=beta", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		Base        string                     `json:"base"`
		Differences []contractDiffFieldPayload `json:"differences"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Base != "beta" {
		t.Fatalf("expected base beta, got %q", payload.Base)
	}

	change, ok := contractDiffByField(payload.Differences, "model_policy.provider")
	if !ok {
		t.Fatalf("expected model_policy.provider diff, got %#v", payload.Differences)
	}
	if change.TargetValue != "openai" || change.BaseValue != "openrouter" {
		t.Fatalf("unexpected provider values in diff: %+v", change)
	}
}

func TestContractRollbackSnapshotRestorePreservesSecrets(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Providers.OpenAI.APIKey = "openai-secret"
	cfg.Discord.Token = "discord-secret"
	cfg.Model.Provider = "hatz"
	cfg.Model.Name = "glm-4.5"
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	snapshotReq := httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/rollback-snapshot", bytes.NewBufferString(`{}`))
	snapshotReq.Header.Set("Content-Type", "application/json")
	snapshotRR := httptest.NewRecorder()
	mux.ServeHTTP(snapshotRR, snapshotReq)

	if snapshotRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, snapshotRR.Code, snapshotRR.Body.String())
	}

	var snapshotPayload struct {
		Snapshot struct {
			ID string `json:"id"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(snapshotRR.Body.Bytes(), &snapshotPayload); err != nil {
		t.Fatalf("decode snapshot response: %v", err)
	}
	if snapshotPayload.Snapshot.ID == "" {
		t.Fatalf("expected snapshot id, got empty payload: %s", snapshotRR.Body.String())
	}

	mutated := cfg
	mutated.Providers.OpenAI.APIKey = ""
	mutated.Discord.Token = ""
	mutated.Model.Provider = "openai"
	mutated.Model.Name = "gpt-4o-mini"
	writeContractDashboardConfig(t, root, mutated)

	restoreReq := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/agents/default/rollback-restore",
		bytes.NewBufferString(`{"snapshot_id":"`+snapshotPayload.Snapshot.ID+`"}`),
	)
	restoreReq.Header.Set("Content-Type", "application/json")
	restoreRR := httptest.NewRecorder()
	mux.ServeHTTP(restoreRR, restoreReq)

	if restoreRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, restoreRR.Code, restoreRR.Body.String())
	}

	restored, err := config.LoadOrDefault(filepath.Join(root, ".openclawssy", "config.json"))
	if err != nil {
		t.Fatalf("load restored config: %v", err)
	}
	if restored.Providers.OpenAI.APIKey != "openai-secret" {
		t.Fatalf("expected openai api key restored, got %q", restored.Providers.OpenAI.APIKey)
	}
	if restored.Discord.Token != "discord-secret" {
		t.Fatalf("expected discord token restored, got %q", restored.Discord.Token)
	}
	if restored.Model.Provider != "hatz" || restored.Model.Name != "glm-4.5" {
		t.Fatalf("expected model restored to hatz/glm-4.5, got %s/%s", restored.Model.Provider, restored.Model.Name)
	}
}

func TestContractRollbackRestoreSnapshotNotFound(t *testing.T) {
	root := t.TempDir()
	writeContractDashboardConfig(t, root, config.Default())

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/rollback-restore", bytes.NewBufferString(`{"snapshot_id":"missing"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d (%s)", http.StatusNotFound, rr.Code, rr.Body.String())
	}
	if got := contractErrorCode(t, rr.Body.Bytes()); got != "contract.snapshot_not_found" {
		t.Fatalf("expected error code contract.snapshot_not_found, got %q", got)
	}
}

func TestInstanceScopedContractResolvedAndDiffEndpointsUseRequestedInstance(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	writeContractDashboardConfig(t, root, cfg)

	h := New(root, httpchannel.NewInMemoryRunStore())
	alphaCfg := config.Default()
	alphaCfg.Agents.Profiles["default"] = config.AgentProfile{Model: config.ModelConfig{Provider: "openai", Name: "gpt-4o-mini"}}
	alphaInstance, err := buildDashboardInstance(instanceBuildInput{ID: "alpha", Name: "Alpha", Config: alphaCfg})
	if err != nil {
		t.Fatalf("build alpha instance: %v", err)
	}
	if err := h.saveProjectedInstance(alphaInstance); err != nil {
		t.Fatalf("save alpha instance: %v", err)
	}

	betaCfg := config.Default()
	betaCfg.Agents.Profiles["default"] = config.AgentProfile{Model: config.ModelConfig{Provider: "openrouter", Name: "moonshot/test"}}
	betaInstance, err := buildDashboardInstance(instanceBuildInput{ID: "beta", Name: "Beta", Config: betaCfg})
	if err != nil {
		t.Fatalf("build beta instance: %v", err)
	}
	if err := h.saveProjectedInstance(betaInstance); err != nil {
		t.Fatalf("save beta instance: %v", err)
	}

	alphaStore, err := h.promptStackStore("alpha")
	if err != nil {
		t.Fatalf("alpha prompt stack store: %v", err)
	}
	betaStore, err := h.promptStackStore("beta")
	if err != nil {
		t.Fatalf("beta prompt stack store: %v", err)
	}
	if _, err := alphaStore.UpdateLayer("default", "agent_identity", "alpha identity"); err != nil {
		t.Fatalf("seed alpha stack: %v", err)
	}
	if _, err := betaStore.UpdateLayer("default", "agent_identity", "beta identity"); err != nil {
		t.Fatalf("seed beta stack: %v", err)
	}

	mux := http.NewServeMux()
	h.Register(mux)

	alphaReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents/default/resolved", nil)
	alphaRR := httptest.NewRecorder()
	mux.ServeHTTP(alphaRR, alphaReq)
	if alphaRR.Code != http.StatusOK {
		t.Fatalf("expected alpha resolved status 200, got %d (%s)", alphaRR.Code, alphaRR.Body.String())
	}
	var alphaPayload agentcontract.AgentContract
	if err := json.Unmarshal(alphaRR.Body.Bytes(), &alphaPayload); err != nil {
		t.Fatalf("decode alpha resolved: %v", err)
	}
	if !strings.Contains(alphaPayload.SystemPrompt.Content, "alpha identity") {
		t.Fatalf("expected alpha prompt stack in resolved contract, got %q", alphaPayload.SystemPrompt.Content)
	}
	if strings.Contains(alphaPayload.SystemPrompt.Content, "beta identity") {
		t.Fatalf("did not expect beta prompt stack content in alpha contract: %q", alphaPayload.SystemPrompt.Content)
	}

	diffReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/beta/agents/default/diff?base=global", nil)
	diffRR := httptest.NewRecorder()
	mux.ServeHTTP(diffRR, diffReq)
	if diffRR.Code != http.StatusOK {
		t.Fatalf("expected beta diff status 200, got %d (%s)", diffRR.Code, diffRR.Body.String())
	}
	betaResolved := getResolvedContractForInstance(t, mux, "beta", "default")
	if !strings.Contains(betaResolved.SystemPrompt.Content, "beta identity") {
		t.Fatalf("expected beta resolved prompt stack, got %q", betaResolved.SystemPrompt.Content)
	}
	var diffPayload struct {
		AgentID     string                     `json:"agent_id"`
		Base        string                     `json:"base"`
		Differences []contractDiffFieldPayload `json:"differences"`
	}
	if err := json.Unmarshal(diffRR.Body.Bytes(), &diffPayload); err != nil {
		t.Fatalf("decode beta diff: %v", err)
	}
	if diffPayload.AgentID != "default" || diffPayload.Base != "global" {
		t.Fatalf("unexpected diff metadata: %+v", diffPayload)
	}
}

type contractDiffFieldPayload struct {
	Field        string `json:"field"`
	TargetValue  any    `json:"target_value"`
	BaseValue    any    `json:"base_value"`
	TargetSource string `json:"target_source"`
	BaseSource   string `json:"base_source"`
}

func contractDiffByField(diffs []contractDiffFieldPayload, field string) (contractDiffFieldPayload, bool) {
	for _, diff := range diffs {
		if diff.Field == field {
			return diff, true
		}
	}
	return contractDiffFieldPayload{}, false
}

func contractErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	return payload.Error.Code
}

func getResolvedContractForAgent(t *testing.T, mux *http.ServeMux, agentID string) agentcontract.AgentContract {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents/"+agentID+"/resolved", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload agentcontract.AgentContract
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

func getResolvedContractForInstance(t *testing.T, mux *http.ServeMux, instanceID, agentID string) agentcontract.AgentContract {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/instances/"+instanceID+"/agents/"+agentID+"/resolved", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload agentcontract.AgentContract
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

func contractHasRoleTemplate(contract agentcontract.AgentContract, roleName string) bool {
	target := strings.ToLower(strings.TrimSpace(roleName))
	for _, template := range contract.DelegationPolicy.RoleTemplates {
		if strings.ToLower(strings.TrimSpace(template.Name)) == target {
			return true
		}
	}
	return false
}

func writeContractDashboardConfig(t *testing.T, root string, cfg config.Config) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".openclawssy"), 0o755); err != nil {
		t.Fatalf("mkdir .openclawssy: %v", err)
	}
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}
