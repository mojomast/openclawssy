package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/config"
	"openclawssy/internal/instances"
	"openclawssy/internal/promptstack"
)

func TestPromptStackGetInitializesFromAgentDocs(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, ".openclawssy", "agents", "default")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("identity line"), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte("allow fs.read only"), 0o600); err != nil {
		t.Fatalf("write RULES.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "TOOLS.md"), []byte("allowed tools: fs.read"), 0o600); err != nil {
		t.Fatalf("write TOOLS.md: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/admin/agents/default/prompt-stack", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		AgentID string                    `json:"agent_id"`
		Layers  []promptstack.PromptLayer `json:"layers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.AgentID != "default" {
		t.Fatalf("expected agent_id default, got %q", payload.AgentID)
	}
	if len(payload.Layers) != len(promptstack.LayerIDs()) {
		t.Fatalf("expected %d layers, got %d", len(promptstack.LayerIDs()), len(payload.Layers))
	}

	layerByID := make(map[string]promptstack.PromptLayer, len(payload.Layers))
	for _, layer := range payload.Layers {
		layerByID[layer.LayerID] = layer
	}
	if !strings.Contains(layerByID[promptstack.LayerAgentIdentity].Content, "identity line") {
		t.Fatalf("expected agent_identity seeded from SOUL.md, got %q", layerByID[promptstack.LayerAgentIdentity].Content)
	}
	if !strings.Contains(layerByID[promptstack.LayerToolSafetyRules].Content, "allow fs.read only") {
		t.Fatalf("expected tool_safety_rules to include RULES.md content, got %q", layerByID[promptstack.LayerToolSafetyRules].Content)
	}
	if layerByID[promptstack.LayerAgentIdentity].Version == 0 {
		t.Fatalf("expected initialized layer version > 0")
	}
}

func TestPromptStackUpdatePreviewHistoryDiffAndRollback(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	put1 := httptest.NewRequest(http.MethodPut, "/api/admin/agents/default/prompt-stack/agent_identity", bytes.NewBufferString(`{"content":"first identity"}`))
	put1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	mux.ServeHTTP(rr1, put1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first PUT status 200, got %d (%s)", rr1.Code, rr1.Body.String())
	}

	put2 := httptest.NewRequest(http.MethodPut, "/api/admin/agents/default/prompt-stack/agent_identity", bytes.NewBufferString(`{"content":"second identity"}`))
	put2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, put2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected second PUT status 200, got %d (%s)", rr2.Code, rr2.Body.String())
	}

	historyRR := httptest.NewRecorder()
	mux.ServeHTTP(historyRR, httptest.NewRequest(http.MethodGet, "/api/admin/agents/default/prompt-stack/history", nil))
	if historyRR.Code != http.StatusOK {
		t.Fatalf("expected history status 200, got %d (%s)", historyRR.Code, historyRR.Body.String())
	}
	var historyPayload struct {
		Layers map[string][]promptstack.PromptLayer `json:"layers"`
	}
	if err := json.Unmarshal(historyRR.Body.Bytes(), &historyPayload); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if len(historyPayload.Layers[promptstack.LayerAgentIdentity]) < 2 {
		t.Fatalf("expected at least 2 agent_identity history entries, got %d", len(historyPayload.Layers[promptstack.LayerAgentIdentity]))
	}

	previewRR := httptest.NewRecorder()
	mux.ServeHTTP(previewRR, httptest.NewRequest(http.MethodGet, "/api/admin/agents/default/prompt-stack/preview", nil))
	if previewRR.Code != http.StatusOK {
		t.Fatalf("expected preview status 200, got %d (%s)", previewRR.Code, previewRR.Body.String())
	}
	var previewPayload struct {
		AssembledPrompt  string `json:"assembled_prompt"`
		TotalTokens      int    `json:"total_tokens"`
		EstimationMethod string `json:"estimation_method"`
	}
	if err := json.Unmarshal(previewRR.Body.Bytes(), &previewPayload); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if !strings.Contains(previewPayload.AssembledPrompt, "second identity") {
		t.Fatalf("expected assembled prompt to contain latest layer content, got %q", previewPayload.AssembledPrompt)
	}
	if previewPayload.TotalTokens <= 0 {
		t.Fatalf("expected total tokens > 0, got %d", previewPayload.TotalTokens)
	}
	if previewPayload.EstimationMethod != promptstack.TokenEstimationMethodWordCountHeuristic {
		t.Fatalf("expected estimation method %q, got %q", promptstack.TokenEstimationMethodWordCountHeuristic, previewPayload.EstimationMethod)
	}

	diffRR := httptest.NewRecorder()
	mux.ServeHTTP(diffRR, httptest.NewRequest(http.MethodGet, "/api/admin/agents/default/prompt-stack/diff?v1=1&v2=2", nil))
	if diffRR.Code != http.StatusOK {
		t.Fatalf("expected diff status 200, got %d (%s)", diffRR.Code, diffRR.Body.String())
	}
	var diffPayload struct {
		Diff promptstack.VersionDiff `json:"diff"`
	}
	if err := json.Unmarshal(diffRR.Body.Bytes(), &diffPayload); err != nil {
		t.Fatalf("decode diff response: %v", err)
	}
	if len(diffPayload.Diff.Lines) == 0 {
		t.Fatalf("expected non-empty diff lines")
	}

	rollbackReq := httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/prompt-stack/rollback", bytes.NewBufferString(`{"version":1}`))
	rollbackReq.Header.Set("Content-Type", "application/json")
	rollbackRR := httptest.NewRecorder()
	mux.ServeHTTP(rollbackRR, rollbackReq)
	if rollbackRR.Code != http.StatusOK {
		t.Fatalf("expected rollback status 200, got %d (%s)", rollbackRR.Code, rollbackRR.Body.String())
	}

	stackRR := httptest.NewRecorder()
	mux.ServeHTTP(stackRR, httptest.NewRequest(http.MethodGet, "/api/admin/agents/default/prompt-stack", nil))
	if stackRR.Code != http.StatusOK {
		t.Fatalf("expected stack status 200 after rollback, got %d (%s)", stackRR.Code, stackRR.Body.String())
	}
	var stackPayload struct {
		Layers []promptstack.PromptLayer `json:"layers"`
	}
	if err := json.Unmarshal(stackRR.Body.Bytes(), &stackPayload); err != nil {
		t.Fatalf("decode stack response: %v", err)
	}
	layerByID := make(map[string]promptstack.PromptLayer, len(stackPayload.Layers))
	for _, layer := range stackPayload.Layers {
		layerByID[layer.LayerID] = layer
	}
	if layerByID[promptstack.LayerAgentIdentity].Content != "first identity" {
		t.Fatalf("expected rollback to restore first identity, got %q", layerByID[promptstack.LayerAgentIdentity].Content)
	}
}

func TestPromptStackLintAndTestEndpoints(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	for _, reqBody := range []struct {
		layer   string
		content string
	}{
		{layer: promptstack.LayerToolSafetyRules, content: "Allow fs.read\nDeny fs.read"},
		{layer: promptstack.LayerDelegationPolicy, content: "maybe delegate this to unicorn"},
		{layer: promptstack.LayerSessionOverlay, content: "stop and return when done\nallowed tools: fs.read\ndelegate to implementer when code changes"},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/agents/default/prompt-stack/"+reqBody.layer, bytes.NewBufferString(`{"content":`+strconvQuote(reqBody.content)+`}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected PUT %s status 200, got %d (%s)", reqBody.layer, rr.Code, rr.Body.String())
		}
	}

	lintRR := httptest.NewRecorder()
	mux.ServeHTTP(lintRR, httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/prompt-stack/lint", bytes.NewBufferString(`{}`)))
	if lintRR.Code != http.StatusOK {
		t.Fatalf("expected lint status 200, got %d (%s)", lintRR.Code, lintRR.Body.String())
	}
	var lintPayload struct {
		Issues []promptstack.LintIssue `json:"issues"`
	}
	if err := json.Unmarshal(lintRR.Body.Bytes(), &lintPayload); err != nil {
		t.Fatalf("decode lint response: %v", err)
	}
	if len(lintPayload.Issues) == 0 {
		t.Fatalf("expected lint to report at least one issue")
	}

	testRR := httptest.NewRecorder()
	mux.ServeHTTP(testRR, httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/prompt-stack/test", bytes.NewBufferString(`{}`)))
	if testRR.Code != http.StatusOK {
		t.Fatalf("expected test status 200, got %d (%s)", testRR.Code, testRR.Body.String())
	}
	var testPayload struct {
		Checks []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(testRR.Body.Bytes(), &testPayload); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	if len(testPayload.Checks) != 3 {
		t.Fatalf("expected 3 structural checks, got %d", len(testPayload.Checks))
	}
}

func TestPromptStackStoreReusesSharedVersionStore(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())

	first, err := h.promptStackStore(instances.DefaultInstanceID)
	if err != nil {
		t.Fatalf("first promptStackStore() error = %v", err)
	}
	second, err := h.promptStackStore(instances.DefaultInstanceID)
	if err != nil {
		t.Fatalf("second promptStackStore() error = %v", err)
	}

	if first != second {
		t.Fatalf("expected promptStackStore() to return a shared store instance")
	}
}

func TestPromptStackConcurrentUpdateAndRollbackMaintainMonotonicHistory(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	initial := httptest.NewRequest(http.MethodPut, "/api/admin/agents/default/prompt-stack/agent_identity", bytes.NewBufferString(`{"content":"seed"}`))
	initial.Header.Set("Content-Type", "application/json")
	initialRR := httptest.NewRecorder()
	mux.ServeHTTP(initialRR, initial)
	if initialRR.Code != http.StatusOK {
		t.Fatalf("expected initial PUT status 200, got %d (%s)", initialRR.Code, initialRR.Body.String())
	}

	const concurrentUpdates = 24
	const concurrentRollbacks = 24
	totalOps := concurrentUpdates + concurrentRollbacks

	start := make(chan struct{})
	errCh := make(chan error, totalOps)
	var wg sync.WaitGroup

	for i := range concurrentUpdates {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			content := fmt.Sprintf("concurrent-update-%02d", i)
			req := httptest.NewRequest(http.MethodPut, "/api/admin/agents/default/prompt-stack/agent_identity", bytes.NewBufferString(`{"content":`+strconvQuote(content)+`}`))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				errCh <- fmt.Errorf("PUT #%d returned %d (%s)", i, rr.Code, rr.Body.String())
			}
		}(i)
	}

	for i := range concurrentRollbacks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			req := httptest.NewRequest(http.MethodPost, "/api/admin/agents/default/prompt-stack/rollback", bytes.NewBufferString(`{"version":1}`))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				errCh <- fmt.Errorf("rollback #%d returned %d (%s)", i, rr.Code, rr.Body.String())
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent prompt-stack request failed: %v", err)
	}

	historyRR := httptest.NewRecorder()
	mux.ServeHTTP(historyRR, httptest.NewRequest(http.MethodGet, "/api/admin/agents/default/prompt-stack/history", nil))
	if historyRR.Code != http.StatusOK {
		t.Fatalf("expected history status 200, got %d (%s)", historyRR.Code, historyRR.Body.String())
	}

	var historyPayload struct {
		Layers map[string][]promptstack.PromptLayer `json:"layers"`
	}
	if err := json.Unmarshal(historyRR.Body.Bytes(), &historyPayload); err != nil {
		t.Fatalf("decode history response: %v", err)
	}

	agentIdentityHistory := historyPayload.Layers[promptstack.LayerAgentIdentity]
	expectedEntries := 1 + totalOps
	if len(agentIdentityHistory) != expectedEntries {
		t.Fatalf("expected %d agent_identity history entries, got %d", expectedEntries, len(agentIdentityHistory))
	}

	for i, layer := range agentIdentityHistory {
		wantVersion := i + 1
		if layer.Version != wantVersion {
			t.Fatalf("expected monotonic version %d at index %d, got %d", wantVersion, i, layer.Version)
		}
	}
}

func TestInstanceScopedPromptStackRoutesIsolateSameAgentID(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	for _, instanceID := range []string{"alpha", "beta"} {
		instance := buildDashboardInstanceOrPanic(t, instanceBuildInput{ID: instanceID, Name: strings.ToUpper(instanceID), Config: config.Default()})
		if err := h.saveProjectedInstance(instance); err != nil {
			t.Fatalf("save projected instance %s: %v", instanceID, err)
		}
	}

	alphaReq := httptest.NewRequest(http.MethodPut, "/api/admin/instances/alpha/agents/default/prompt-stack/agent_identity", bytes.NewBufferString(`{"content":"alpha identity"}`))
	alphaReq.Header.Set("Content-Type", "application/json")
	alphaRR := httptest.NewRecorder()
	mux.ServeHTTP(alphaRR, alphaReq)
	if alphaRR.Code != http.StatusOK {
		t.Fatalf("expected alpha PUT status 200, got %d (%s)", alphaRR.Code, alphaRR.Body.String())
	}

	betaReq := httptest.NewRequest(http.MethodPut, "/api/admin/instances/beta/agents/default/prompt-stack/agent_identity", bytes.NewBufferString(`{"content":"beta identity"}`))
	betaReq.Header.Set("Content-Type", "application/json")
	betaRR := httptest.NewRecorder()
	mux.ServeHTTP(betaRR, betaReq)
	if betaRR.Code != http.StatusOK {
		t.Fatalf("expected beta PUT status 200, got %d (%s)", betaRR.Code, betaRR.Body.String())
	}

	alphaGet := httptest.NewRecorder()
	mux.ServeHTTP(alphaGet, httptest.NewRequest(http.MethodGet, "/api/admin/instances/alpha/agents/default/prompt-stack", nil))
	if alphaGet.Code != http.StatusOK {
		t.Fatalf("expected alpha GET status 200, got %d (%s)", alphaGet.Code, alphaGet.Body.String())
	}
	betaGet := httptest.NewRecorder()
	mux.ServeHTTP(betaGet, httptest.NewRequest(http.MethodGet, "/api/admin/instances/beta/agents/default/prompt-stack", nil))
	if betaGet.Code != http.StatusOK {
		t.Fatalf("expected beta GET status 200, got %d (%s)", betaGet.Code, betaGet.Body.String())
	}

	var alphaPayload struct {
		InstanceID string                    `json:"instance_id"`
		Layers     []promptstack.PromptLayer `json:"layers"`
	}
	if err := json.Unmarshal(alphaGet.Body.Bytes(), &alphaPayload); err != nil {
		t.Fatalf("decode alpha response: %v", err)
	}
	var betaPayload struct {
		InstanceID string                    `json:"instance_id"`
		Layers     []promptstack.PromptLayer `json:"layers"`
	}
	if err := json.Unmarshal(betaGet.Body.Bytes(), &betaPayload); err != nil {
		t.Fatalf("decode beta response: %v", err)
	}
	if alphaPayload.InstanceID != "alpha" || betaPayload.InstanceID != "beta" {
		t.Fatalf("unexpected instance ids: alpha=%q beta=%q", alphaPayload.InstanceID, betaPayload.InstanceID)
	}
	alphaLayers := map[string]promptstack.PromptLayer{}
	for _, layer := range alphaPayload.Layers {
		alphaLayers[layer.LayerID] = layer
	}
	betaLayers := map[string]promptstack.PromptLayer{}
	for _, layer := range betaPayload.Layers {
		betaLayers[layer.LayerID] = layer
	}
	if alphaLayers[promptstack.LayerAgentIdentity].Content != "alpha identity" {
		t.Fatalf("expected alpha identity isolation, got %q", alphaLayers[promptstack.LayerAgentIdentity].Content)
	}
	if betaLayers[promptstack.LayerAgentIdentity].Content != "beta identity" {
		t.Fatalf("expected beta identity isolation, got %q", betaLayers[promptstack.LayerAgentIdentity].Content)
	}
}

func buildDashboardInstanceOrPanic(t *testing.T, input instanceBuildInput) dashboardInstance {
	t.Helper()
	instance, err := buildDashboardInstance(input)
	if err != nil {
		t.Fatalf("build dashboard instance: %v", err)
	}
	return instance
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
