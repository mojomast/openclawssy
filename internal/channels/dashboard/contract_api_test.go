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
	agentcontract "openclawssy/internal/contract"
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

func writeContractDashboardConfig(t *testing.T, root string, cfg config.Config) {
	t.Helper()
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}
