package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/tools"
)

type stubInboxAgentRunner struct{}

func (stubInboxAgentRunner) ExecuteSubAgent(_ context.Context, input tools.AgentRunInput) (tools.AgentRunOutput, error) {
	return tools.AgentRunOutput{RunID: "run-inbox-1", FinalText: "done", Status: tools.AgentMessageStatusCompleted, MessageID: input.MessageID}, nil
}

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
	if templatesRR.Code != http.StatusForbidden {
		t.Fatalf("expected templates status %d, got %d (%s)", http.StatusForbidden, templatesRR.Code, templatesRR.Body.String())
	}
	assertDashboardErrorCode(t, templatesRR.Body.Bytes(), "feature.wizard_disabled")

	getFeaturesReq := httptest.NewRequest(http.MethodGet, "/api/admin/control-plane/features", nil)
	getFeaturesRR := httptest.NewRecorder()
	mux.ServeHTTP(getFeaturesRR, getFeaturesReq)
	if getFeaturesRR.Code != http.StatusOK {
		t.Fatalf("expected control plane features status %d, got %d (%s)", http.StatusOK, getFeaturesRR.Code, getFeaturesRR.Body.String())
	}

	patchFeaturesReq = httptest.NewRequest(http.MethodPatch, "/api/admin/control-plane/features", bytes.NewBufferString(`{"wizard":true}`))
	patchFeaturesRR = httptest.NewRecorder()
	mux.ServeHTTP(patchFeaturesRR, patchFeaturesReq)
	if patchFeaturesRR.Code != http.StatusOK {
		t.Fatalf("expected patch features re-enable status %d, got %d (%s)", http.StatusOK, patchFeaturesRR.Code, patchFeaturesRR.Body.String())
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
	var createdPayload struct {
		Instance map[string]any `json:"instance"`
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/instances/create", bytes.NewBufferString(instanceWizardBody))
	createRR := httptest.NewRecorder()
	mux.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("expected wizard instance create status %d, got %d (%s)", http.StatusCreated, createRR.Code, createRR.Body.String())
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &createdPayload); err != nil {
		t.Fatalf("decode create payload: %v", err)
	}
	createdConfig, ok := createdPayload.Instance["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config in create payload, got %#v", createdPayload.Instance)
	}
	createdModel, ok := createdConfig["model"].(map[string]any)
	if !ok {
		t.Fatalf("expected model in create payload config, got %#v", createdConfig)
	}
	if createdModel["provider"] == "" || createdModel["name"] == "" {
		t.Fatalf("expected created model provider/name to be populated, got %#v", createdModel)
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
	var agentCreatePayload struct {
		Agent struct {
			AgentID string              `json:"agent_id"`
			Profile config.AgentProfile `json:"profile"`
		} `json:"agent"`
	}

	agentCreateReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/agents/create", bytes.NewBufferString(agentWizardBody))
	agentCreateRR := httptest.NewRecorder()
	mux.ServeHTTP(agentCreateRR, agentCreateReq)
	if agentCreateRR.Code != http.StatusCreated {
		t.Fatalf("expected wizard agent create status %d, got %d (%s)", http.StatusCreated, agentCreateRR.Code, agentCreateRR.Body.String())
	}
	if err := json.Unmarshal(agentCreateRR.Body.Bytes(), &agentCreatePayload); err != nil {
		t.Fatalf("decode agent create payload: %v", err)
	}
	if agentCreatePayload.Agent.AgentID != agentPlanPayload.Plan.AgentID {
		t.Fatalf("expected created agent id %q to match plan %q", agentCreatePayload.Agent.AgentID, agentPlanPayload.Plan.AgentID)
	}
	if agentCreatePayload.Agent.Profile.Model != agentPlanPayload.Plan.Profile.Model {
		t.Fatalf("expected created agent model to match plan\nplan=%#v\ncreate=%#v", agentPlanPayload.Plan.Profile.Model, agentCreatePayload.Agent.Profile.Model)
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

func TestWizardEndpointsRequireFeatureSpecificGuards(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/control-plane/features", bytes.NewBufferString(`{"instance_control":false,"instance_agents":false,"wizard":true}`))
	patchRR := httptest.NewRecorder()
	mux.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusOK {
		t.Fatalf("expected feature patch status %d, got %d (%s)", http.StatusOK, patchRR.Code, patchRR.Body.String())
	}

	instancePlanReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/instances/plan", bytes.NewBufferString(`{"template_id":"blank","instance_id":"alpha"}`))
	instancePlanRR := httptest.NewRecorder()
	mux.ServeHTTP(instancePlanRR, instancePlanReq)
	if instancePlanRR.Code != http.StatusForbidden {
		t.Fatalf("expected wizard instance plan status %d, got %d (%s)", http.StatusForbidden, instancePlanRR.Code, instancePlanRR.Body.String())
	}
	assertDashboardErrorCode(t, instancePlanRR.Body.Bytes(), "feature.instance_control_disabled")

	instanceCreateReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/instances/create", bytes.NewBufferString(`{"template_id":"blank","instance_id":"alpha"}`))
	instanceCreateRR := httptest.NewRecorder()
	mux.ServeHTTP(instanceCreateRR, instanceCreateReq)
	if instanceCreateRR.Code != http.StatusForbidden {
		t.Fatalf("expected wizard instance create status %d, got %d (%s)", http.StatusForbidden, instanceCreateRR.Code, instanceCreateRR.Body.String())
	}
	assertDashboardErrorCode(t, instanceCreateRR.Body.Bytes(), "feature.instance_control_disabled")

	agentPlanReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/agents/plan", bytes.NewBufferString(`{"instance_id":"alpha","agent_id":"researcher","template_id":"research"}`))
	agentPlanRR := httptest.NewRecorder()
	mux.ServeHTTP(agentPlanRR, agentPlanReq)
	if agentPlanRR.Code != http.StatusForbidden {
		t.Fatalf("expected wizard agent plan status %d, got %d (%s)", http.StatusForbidden, agentPlanRR.Code, agentPlanRR.Body.String())
	}
	assertDashboardErrorCode(t, agentPlanRR.Body.Bytes(), "feature.instance_agents_disabled")

	agentCreateReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/agents/create", bytes.NewBufferString(`{"instance_id":"alpha","agent_id":"researcher","template_id":"research"}`))
	agentCreateRR := httptest.NewRecorder()
	mux.ServeHTTP(agentCreateRR, agentCreateReq)
	if agentCreateRR.Code != http.StatusForbidden {
		t.Fatalf("expected wizard agent create status %d, got %d (%s)", http.StatusForbidden, agentCreateRR.Code, agentCreateRR.Body.String())
	}
	assertDashboardErrorCode(t, agentCreateRR.Body.Bytes(), "feature.instance_agents_disabled")
}

func TestWizardAgentCreateRejectsDuplicateAgent(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	createInstanceReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances", bytes.NewBufferString(`{"id":"wizard-one","name":"Wizard One"}`))
	createInstanceRR := httptest.NewRecorder()
	mux.ServeHTTP(createInstanceRR, createInstanceReq)
	if createInstanceRR.Code != http.StatusCreated {
		t.Fatalf("expected create instance status %d, got %d (%s)", http.StatusCreated, createInstanceRR.Code, createInstanceRR.Body.String())
	}

	createAgentReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/wizard-one/agents", bytes.NewBufferString(`{"agent_id":"researcher","profile":{"enabled":true}}`))
	createAgentRR := httptest.NewRecorder()
	mux.ServeHTTP(createAgentRR, createAgentReq)
	if createAgentRR.Code != http.StatusCreated {
		t.Fatalf("expected create agent status %d, got %d (%s)", http.StatusCreated, createAgentRR.Code, createAgentRR.Body.String())
	}

	wizardReq := httptest.NewRequest(http.MethodPost, "/api/admin/wizard/agents/create", bytes.NewBufferString(`{"instance_id":"wizard-one","agent_id":"researcher","template_id":"research"}`))
	wizardRR := httptest.NewRecorder()
	mux.ServeHTTP(wizardRR, wizardReq)
	if wizardRR.Code != http.StatusConflict {
		t.Fatalf("expected duplicate agent status %d, got %d (%s)", http.StatusConflict, wizardRR.Code, wizardRR.Body.String())
	}
	assertDashboardErrorCode(t, wizardRR.Body.Bytes(), "instances.agent_exists")
}

func TestInstanceFeatureFlagEnforcement(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/control-plane/features", bytes.NewBufferString(`{"instance_control":false,"instance_agents":false}`))
	patchRR := httptest.NewRecorder()
	mux.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusOK {
		t.Fatalf("expected feature patch status %d, got %d (%s)", http.StatusOK, patchRR.Code, patchRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusForbidden {
		t.Fatalf("expected instances list status %d, got %d (%s)", http.StatusForbidden, listRR.Code, listRR.Body.String())
	}
	assertDashboardErrorCode(t, listRR.Body.Bytes(), "feature.instance_control_disabled")

	agentReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents", nil)
	agentRR := httptest.NewRecorder()
	mux.ServeHTTP(agentRR, agentReq)
	if agentRR.Code != http.StatusForbidden {
		t.Fatalf("expected instance agents status %d, got %d (%s)", http.StatusForbidden, agentRR.Code, agentRR.Body.String())
	}
	assertDashboardErrorCode(t, agentRR.Body.Bytes(), "feature.instance_agents_disabled")
}

func TestInstanceInboxListAckAndRunFlow(t *testing.T) {
	root := t.TempDir()
	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{AgentRunner: stubInboxAgentRunner{}})
	mux := http.NewServeMux()
	h.Register(mux)

	createInstanceReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances", bytes.NewBufferString(`{"id":"alpha","name":"Alpha"}`))
	createInstanceRR := httptest.NewRecorder()
	mux.ServeHTTP(createInstanceRR, createInstanceReq)
	if createInstanceRR.Code != http.StatusCreated {
		t.Fatalf("expected create instance status %d, got %d (%s)", http.StatusCreated, createInstanceRR.Code, createInstanceRR.Body.String())
	}

	createAgentReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/alpha/agents", bytes.NewBufferString(`{"agent_id":"receiver","profile":{"enabled":true}}`))
	createAgentRR := httptest.NewRecorder()
	mux.ServeHTTP(createAgentRR, createAgentReq)
	if createAgentRR.Code != http.StatusCreated {
		t.Fatalf("expected create agent status %d, got %d (%s)", http.StatusCreated, createAgentRR.Code, createAgentRR.Body.String())
	}

	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "receiver", Channel: "agent-mail", UserID: "sender", RoomID: "task-1"})
	if err != nil {
		t.Fatalf("create inbox session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, chatstore.Message{
		Role:        "user",
		Content:     `{"message_id":"msg-1","status":"queued","instance_id":"alpha","from_agent_id":"sender","to_agent_id":"receiver","message":"hello","task_id":"task-1"}`,
		MessageID:   "msg-1",
		Status:      "queued",
		InstanceID:  "alpha",
		FromAgentID: "sender",
		ToAgentID:   "receiver",
		TaskID:      "task-1",
	}); err != nil {
		t.Fatalf("append inbox message: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents/inbox", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected inbox list status %d, got %d (%s)", http.StatusOK, listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Messages []struct {
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
			Message   string `json:"message"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if len(listPayload.Messages) != 1 || listPayload.Messages[0].MessageID != "msg-1" || listPayload.Messages[0].Status != "queued" {
		t.Fatalf("unexpected inbox list payload: %+v", listPayload.Messages)
	}
	if listPayload.Messages[0].Message != "hello" {
		t.Fatalf("expected original message in list payload, got %+v", listPayload.Messages[0])
	}

	ackReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/alpha/agents/receiver/inbox/msg-1/ack", bytes.NewBufferString(`{}`))
	ackRR := httptest.NewRecorder()
	mux.ServeHTTP(ackRR, ackReq)
	if ackRR.Code != http.StatusOK {
		t.Fatalf("expected inbox ack status %d, got %d (%s)", http.StatusOK, ackRR.Code, ackRR.Body.String())
	}
	var ackPayload struct {
		Message struct {
			Status  string `json:"status"`
			Note    string `json:"note"`
			Message string `json:"message"`
		} `json:"message"`
	}
	if err := json.Unmarshal(ackRR.Body.Bytes(), &ackPayload); err != nil {
		t.Fatalf("decode ack payload: %v", err)
	}
	if ackPayload.Message.Status != "acknowledged" {
		t.Fatalf("expected acknowledged status, got %+v", ackPayload.Message)
	}
	if ackPayload.Message.Note == "" {
		t.Fatalf("expected ack note, got %+v", ackPayload.Message)
	}
	if ackPayload.Message.Message != "hello" {
		t.Fatalf("expected original message after ack, got %+v", ackPayload.Message)
	}
	if _, err := h.loadInstanceInboxMessage(store, "alpha", "receiver", "msg-1"); err != nil {
		t.Fatalf("load inbox message after ack: %v", err)
	}

	runReq := httptest.NewRequest(http.MethodPost, "/api/admin/instances/alpha/agents/receiver/inbox/msg-1/run", bytes.NewBufferString(`{"source":"dashboard/test"}`))
	runRR := httptest.NewRecorder()
	mux.ServeHTTP(runRR, runReq)
	if runRR.Code != http.StatusOK {
		t.Fatalf("expected inbox run status %d, got %d (%s)", http.StatusOK, runRR.Code, runRR.Body.String())
	}
	var runPayload struct {
		RunID   string `json:"run_id"`
		Status  string `json:"status"`
		Message struct {
			Status       string `json:"status"`
			RelatedRunID string `json:"related_run_id"`
			Message      string `json:"message"`
		} `json:"message"`
	}
	if err := json.Unmarshal(runRR.Body.Bytes(), &runPayload); err != nil {
		t.Fatalf("decode run payload: %v", err)
	}
	if runPayload.RunID != "run-inbox-1" || runPayload.Status != "completed" {
		t.Fatalf("unexpected run payload: %+v", runPayload)
	}
	if runPayload.Message.Status != "completed" || runPayload.Message.RelatedRunID != "run-inbox-1" {
		t.Fatalf("expected completed lifecycle message, got %+v", runPayload.Message)
	}
	if runPayload.Message.Message != "hello" {
		t.Fatalf("expected original message after run, got %+v", runPayload.Message)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents/receiver/inbox/msg-1", nil)
	detailRR := httptest.NewRecorder()
	mux.ServeHTTP(detailRR, detailReq)
	if detailRR.Code != http.StatusOK {
		t.Fatalf("expected inbox detail status %d, got %d (%s)", http.StatusOK, detailRR.Code, detailRR.Body.String())
	}
	var detailPayload struct {
		Message struct {
			Message         string `json:"message"`
			Status          string `json:"status"`
			FromAgentID     string `json:"from_agent_id"`
			TaskID          string `json:"task_id"`
			RelatedRunID    string `json:"related_run_id"`
			SourceSessionID string `json:"source_session_id"`
		} `json:"message"`
	}
	if err := json.Unmarshal(detailRR.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode detail payload: %v", err)
	}
	if detailPayload.Message.Message != "hello" || detailPayload.Message.Status != "completed" || detailPayload.Message.FromAgentID != "sender" || detailPayload.Message.TaskID != "task-1" || detailPayload.Message.RelatedRunID != "run-inbox-1" {
		t.Fatalf("unexpected inbox detail payload: %+v", detailPayload.Message)
	}
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
