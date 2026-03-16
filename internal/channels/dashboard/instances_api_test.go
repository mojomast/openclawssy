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
)

func TestInstancesBootstrapCloneActivateAndDeleteFlow(t *testing.T) {
	root := t.TempDir()
	current := config.Default()
	current.Model.Provider = "openai"
	current.Model.Name = "gpt-5-mini"
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), current); err != nil {
		t.Fatalf("save current config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	bootstrapReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/bootstrap-from-current", bytes.NewBufferString(`{"id":"primary","name":"Primary"}`))
	bootstrapRR := httptest.NewRecorder()
	mux.ServeHTTP(bootstrapRR, bootstrapReq)
	if bootstrapRR.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status %d, got %d (%s)", http.StatusCreated, bootstrapRR.Code, bootstrapRR.Body.String())
	}

	activeReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/active", nil)
	activeRR := httptest.NewRecorder()
	mux.ServeHTTP(activeRR, activeReq)
	if activeRR.Code != http.StatusOK {
		t.Fatalf("expected active status %d, got %d (%s)", http.StatusOK, activeRR.Code, activeRR.Body.String())
	}
	var activePayload struct {
		Instance map[string]any `json:"instance"`
	}
	if err := json.Unmarshal(activeRR.Body.Bytes(), &activePayload); err != nil {
		t.Fatalf("decode active payload: %v", err)
	}
	if activePayload.Instance["id"] != "primary" {
		t.Fatalf("expected active instance primary, got %#v", activePayload.Instance)
	}

	cloneReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/primary/clone", bytes.NewBufferString(`{"id":"staging","name":"Staging"}`))
	cloneRR := httptest.NewRecorder()
	mux.ServeHTTP(cloneRR, cloneReq)
	if cloneRR.Code != http.StatusCreated {
		t.Fatalf("expected clone status %d, got %d (%s)", http.StatusCreated, cloneRR.Code, cloneRR.Body.String())
	}

	activateReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/staging/activate", bytes.NewBufferString(`{}`))
	activateRR := httptest.NewRecorder()
	mux.ServeHTTP(activateRR, activateReq)
	if activateRR.Code != http.StatusOK {
		t.Fatalf("expected activate status %d, got %d (%s)", http.StatusOK, activateRR.Code, activateRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d (%s)", http.StatusOK, listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Instances        []map[string]any `json:"instances"`
		ActiveInstanceID string           `json:"active_instance_id"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if listPayload.ActiveInstanceID != "staging" {
		t.Fatalf("expected active_instance_id staging, got %q", listPayload.ActiveInstanceID)
	}
	if len(listPayload.Instances) != 2 {
		t.Fatalf("expected two instances, got %#v", listPayload.Instances)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/instances/primary", nil)
	deleteRR := httptest.NewRecorder()
	mux.ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("expected delete status %d, got %d (%s)", http.StatusOK, deleteRR.Code, deleteRR.Body.String())
	}

	configAfterActivate, err := config.LoadOrDefault(filepath.Join(root, ".openclawssy", "config.json"))
	if err != nil {
		t.Fatalf("load activated config: %v", err)
	}
	if configAfterActivate.Model.Provider != "openai" || configAfterActivate.Model.Name != "gpt-5-mini" {
		t.Fatalf("expected activated config to preserve cloned model, got provider=%q model=%q", configAfterActivate.Model.Provider, configAfterActivate.Model.Name)
	}
}

func TestInstanceAgentCRUD(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	createInstanceReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances", bytes.NewBufferString(`{"id":"alpha","name":"Alpha"}`))
	createInstanceRR := httptest.NewRecorder()
	mux.ServeHTTP(createInstanceRR, createInstanceReq)
	if createInstanceRR.Code != http.StatusCreated {
		t.Fatalf("expected create instance status %d, got %d (%s)", http.StatusCreated, createInstanceRR.Code, createInstanceRR.Body.String())
	}

	createAgentReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/alpha/agents", bytes.NewBufferString(`{"agent_id":"reviewer","profile":{"enabled":true,"self_improvement":true,"model":{"provider":"openai","name":"gpt-4.1-mini","timeout_ms":180000}}}`))
	createAgentRR := httptest.NewRecorder()
	mux.ServeHTTP(createAgentRR, createAgentReq)
	if createAgentRR.Code != http.StatusCreated {
		t.Fatalf("expected create agent status %d, got %d (%s)", http.StatusCreated, createAgentRR.Code, createAgentRR.Body.String())
	}

	getAgentReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents/reviewer", nil)
	getAgentRR := httptest.NewRecorder()
	mux.ServeHTTP(getAgentRR, getAgentReq)
	if getAgentRR.Code != http.StatusOK {
		t.Fatalf("expected get agent status %d, got %d (%s)", http.StatusOK, getAgentRR.Code, getAgentRR.Body.String())
	}
	var getAgentPayload struct {
		Agent struct {
			AgentID string              `json:"agent_id"`
			Profile config.AgentProfile `json:"profile"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(getAgentRR.Body.Bytes(), &getAgentPayload); err != nil {
		t.Fatalf("decode agent payload: %v", err)
	}
	if getAgentPayload.Agent.AgentID != "reviewer" || getAgentPayload.Agent.Profile.Model.Provider != "openai" {
		t.Fatalf("unexpected agent payload: %#v", getAgentPayload.Agent)
	}

	updateAgentReq := httptest.NewRequest(http.MethodPut, "/api/admin/instances/alpha/agents/reviewer", bytes.NewBufferString(`{"agent_id":"reviewer","profile":{"enabled":false,"model":{"provider":"zai","name":"glm-4.7","timeout_ms":120000}}}`))
	updateAgentRR := httptest.NewRecorder()
	mux.ServeHTTP(updateAgentRR, updateAgentReq)
	if updateAgentRR.Code != http.StatusOK {
		t.Fatalf("expected update agent status %d, got %d (%s)", http.StatusOK, updateAgentRR.Code, updateAgentRR.Body.String())
	}

	listAgentsReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents", nil)
	listAgentsRR := httptest.NewRecorder()
	mux.ServeHTTP(listAgentsRR, listAgentsReq)
	if listAgentsRR.Code != http.StatusOK {
		t.Fatalf("expected list agents status %d, got %d (%s)", http.StatusOK, listAgentsRR.Code, listAgentsRR.Body.String())
	}

	deleteAgentReq := httptest.NewRequest(http.MethodDelete, "/api/admin/instances/alpha/agents/reviewer", nil)
	deleteAgentRR := httptest.NewRecorder()
	mux.ServeHTTP(deleteAgentRR, deleteAgentReq)
	if deleteAgentRR.Code != http.StatusOK {
		t.Fatalf("expected delete agent status %d, got %d (%s)", http.StatusOK, deleteAgentRR.Code, deleteAgentRR.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents/reviewer", nil)
	missingRR := httptest.NewRecorder()
	mux.ServeHTTP(missingRR, missingReq)
	if missingRR.Code != http.StatusNotFound {
		t.Fatalf("expected missing agent status %d, got %d (%s)", http.StatusNotFound, missingRR.Code, missingRR.Body.String())
	}
	assertDashboardErrorCode(t, missingRR.Body.Bytes(), "instances.agent_not_found")
}

func TestControlPlaneFeaturesAndWizardEndpoints(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	patchFeaturesReq := httptest.NewRequest(http.MethodPatch, "/api/admin/control-plane/features", bytes.NewBufferString(`{"wizard":false}`))
	patchFeaturesRR := httptest.NewRecorder()
	mux.ServeHTTP(patchFeaturesRR, patchFeaturesReq)
	if patchFeaturesRR.Code != http.StatusOK {
		t.Fatalf("expected patch features status %d, got %d (%s)", http.StatusOK, patchFeaturesRR.Code, patchFeaturesRR.Body.String())
	}

	templatesReq := httptest.NewRequest(http.MethodGet, "/api/admin/wizard/templates", nil)
	templatesRR := httptest.NewRecorder()
	mux.ServeHTTP(templatesRR, templatesReq)
	if templatesRR.Code != http.StatusOK {
		t.Fatalf("expected templates status %d, got %d (%s)", http.StatusOK, templatesRR.Code, templatesRR.Body.String())
	}

	instanceWizardBody := `{"template_id":"chat-assistant","instance_id":"wizard-one","name":"Wizard One","default_agent_id":"assistant","model_provider":"openrouter","model_name":"moonshot/test"}`
	planReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/instances/plan", bytes.NewBufferString(instanceWizardBody))
	planRR := httptest.NewRecorder()
	mux.ServeHTTP(planRR, planReq)
	if planRR.Code != http.StatusOK {
		t.Fatalf("expected wizard instance plan status %d, got %d (%s)", http.StatusOK, planRR.Code, planRR.Body.String())
	}
	var planPayload struct {
		Plan struct {
			Instance map[string]any `json:"instance"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(planRR.Body.Bytes(), &planPayload); err != nil {
		t.Fatalf("decode plan payload: %v", err)
	}
	planConfig, ok := planPayload.Plan.Instance["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config in plan payload, got %#v", planPayload.Plan.Instance)
	}
	model, ok := planConfig["model"].(map[string]any)
	if !ok || model["provider"] != "openrouter" {
		t.Fatalf("expected planned model provider openrouter, got %#v", planConfig)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/instances/create", bytes.NewBufferString(instanceWizardBody))
	createRR := httptest.NewRecorder()
	mux.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("expected wizard instance create status %d, got %d (%s)", http.StatusCreated, createRR.Code, createRR.Body.String())
	}

	agentWizardBody := `{"instance_id":"wizard-one","agent_id":"researcher","template_id":"research","model_provider":"openai","model_name":"gpt-4.1-mini"}`
	agentPlanReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/agents/plan", bytes.NewBufferString(agentWizardBody))
	agentPlanRR := httptest.NewRecorder()
	mux.ServeHTTP(agentPlanRR, agentPlanReq)
	if agentPlanRR.Code != http.StatusOK {
		t.Fatalf("expected wizard agent plan status %d, got %d (%s)", http.StatusOK, agentPlanRR.Code, agentPlanRR.Body.String())
	}
	var agentPlanPayload struct {
		Plan struct {
			AgentID string `json:"agent_id"`
			Profile struct {
				Model config.ModelConfig `json:"model"`
			} `json:"profile"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(agentPlanRR.Body.Bytes(), &agentPlanPayload); err != nil {
		t.Fatalf("decode agent plan payload: %v", err)
	}
	if agentPlanPayload.Plan.AgentID != "researcher" || agentPlanPayload.Plan.Profile.Model.Provider != "openai" {
		t.Fatalf("unexpected agent plan payload: %#v", agentPlanPayload.Plan)
	}

	agentCreateReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/agents/create", bytes.NewBufferString(agentWizardBody))
	agentCreateRR := httptest.NewRecorder()
	mux.ServeHTTP(agentCreateRR, agentCreateReq)
	if agentCreateRR.Code != http.StatusCreated {
		t.Fatalf("expected wizard agent create status %d, got %d (%s)", http.StatusCreated, agentCreateRR.Code, agentCreateRR.Body.String())
	}

	instanceReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/wizard-one", nil)
	instanceRR := httptest.NewRecorder()
	mux.ServeHTTP(instanceRR, instanceReq)
	if instanceRR.Code != http.StatusOK {
		t.Fatalf("expected get instance status %d, got %d (%s)", http.StatusOK, instanceRR.Code, instanceRR.Body.String())
	}
	var instancePayload struct {
		Instance struct {
			Config struct {
				Agents struct {
					Profiles map[string]config.AgentProfile `json:"profiles"`
				} `json:"agents"`
			} `json:"config"`
		} `json:"instance"`
	}
	if err := json.Unmarshal(instanceRR.Body.Bytes(), &instancePayload); err != nil {
		t.Fatalf("decode instance payload: %v", err)
	}
	profile, ok := instancePayload.Instance.Config.Agents.Profiles["researcher"]
	if !ok {
		t.Fatalf("expected researcher profile in created instance, got %#v", instancePayload.Instance.Config.Agents.Profiles)
	}
	if profile.Model.Provider != agentPlanPayload.Plan.Profile.Model.Provider || profile.Model.Name != agentPlanPayload.Plan.Profile.Model.Name {
		t.Fatalf("expected create to match planned profile, got %#v", profile)
	}

	invalidReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/instances/plan", bytes.NewBufferString(`{"template_id":"unknown","instance_id":"bad"}`))
	invalidRR := httptest.NewRecorder()
	mux.ServeHTTP(invalidRR, invalidReq)
	if invalidRR.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid template status %d, got %d (%s)", http.StatusBadRequest, invalidRR.Code, invalidRR.Body.String())
	}
	assertDashboardErrorCode(t, invalidRR.Body.Bytes(), "wizard.unknown_instance_template")
}

func assertDashboardErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Error.Code != want {
		t.Fatalf("expected error code %q, got %q", want, payload.Error.Code)
	}
}
