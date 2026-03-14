package promptstack

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPromptStackHasFiveNamedLayers(t *testing.T) {
	t.Parallel()

	stack := NewPromptStack()
	layers := stack.LayersInOrder()

	if len(layers) != 5 {
		t.Fatalf("expected 5 layers, got %d", len(layers))
	}

	wantIDs := []string{
		LayerGlobalOperatorPolicy,
		LayerAgentIdentity,
		LayerToolSafetyRules,
		LayerDelegationPolicy,
		LayerSessionOverlay,
	}

	for i, want := range wantIDs {
		if layers[i].LayerID != want {
			t.Fatalf("layer %d id = %q, want %q", i, layers[i].LayerID, want)
		}
		if layers[i].Version != 0 {
			t.Fatalf("layer %q version = %d, want 0", layers[i].LayerID, layers[i].Version)
		}
	}
}

func TestAssembleMergesLayersTopToBottomWithMarkers(t *testing.T) {
	t.Parallel()

	stack := NewPromptStack()
	stack.GlobalOperatorPolicy.Content = "global policy"
	stack.AgentIdentity.Content = "identity policy"
	stack.ToolSafetyRules.Content = "tool safety"
	stack.DelegationPolicy.Content = "delegation guidance"
	stack.SessionOverlay.Content = "session overlay"

	assembled := Assemble(stack)

	markers := []string{
		"<<<LAYER: global_operator_policy>>>",
		"<<<LAYER: agent_identity>>>",
		"<<<LAYER: tool_safety_rules>>>",
		"<<<LAYER: delegation_policy>>>",
		"<<<LAYER: session_overlay>>>",
	}

	lastIndex := -1
	for _, marker := range markers {
		idx := strings.Index(assembled, marker)
		if idx < 0 {
			t.Fatalf("assembled prompt missing marker %q", marker)
		}
		if idx <= lastIndex {
			t.Fatalf("marker %q appears out of order", marker)
		}
		lastIndex = idx
	}

	for _, content := range []string{"global policy", "identity policy", "tool safety", "delegation guidance", "session overlay"} {
		if !strings.Contains(assembled, content) {
			t.Fatalf("assembled prompt missing content %q", content)
		}
	}
}

func TestEstimateTokensReturnsPerLayerAndTotal(t *testing.T) {
	t.Parallel()

	stack := NewPromptStack()
	stack.GlobalOperatorPolicy.Content = "one two"
	stack.AgentIdentity.Content = "three"
	stack.ToolSafetyRules.Content = "four five six seven"
	stack.DelegationPolicy.Content = ""
	stack.SessionOverlay.Content = "eight nine ten"

	estimate := EstimateTokens(stack)

	if estimate.EstimationMethod != TokenEstimationMethodWordCountHeuristic {
		t.Fatalf("estimation method = %q, want %q", estimate.EstimationMethod, TokenEstimationMethodWordCountHeuristic)
	}

	byLayer := make(map[string]LayerTokenEstimate, len(estimate.PerLayer))
	for _, layer := range estimate.PerLayer {
		byLayer[layer.LayerID] = layer
	}

	wantTokens := map[string]int{
		LayerGlobalOperatorPolicy: 3, // ceil(2*1.3)
		LayerAgentIdentity:        2, // ceil(1*1.3)
		LayerToolSafetyRules:      6, // ceil(4*1.3)
		LayerDelegationPolicy:     0, // ceil(0*1.3)
		LayerSessionOverlay:       4, // ceil(3*1.3)
	}

	total := 0
	for layerID, want := range wantTokens {
		got, ok := byLayer[layerID]
		if !ok {
			t.Fatalf("missing layer estimate for %q", layerID)
		}
		if got.TokenCount != want {
			t.Fatalf("layer %q token count = %d, want %d", layerID, got.TokenCount, want)
		}
		total += want
	}

	if estimate.TotalTokens != total {
		t.Fatalf("total tokens = %d, want %d", estimate.TotalTokens, total)
	}
}

func TestVersionStoreUpdateHistoryGetVersionDiffAndRollback(t *testing.T) {
	t.Parallel()

	store, err := NewVersionStore(filepath.Join(t.TempDir(), ".openclawssy"))
	if err != nil {
		t.Fatalf("NewVersionStore() error = %v", err)
	}

	agentID := "alpha"
	layerID := LayerToolSafetyRules

	v1, err := store.UpdateLayer(agentID, layerID, "line one\nline two\n")
	if err != nil {
		t.Fatalf("UpdateLayer() v1 error = %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("v1 version = %d, want 1", v1.Version)
	}

	v2, err := store.UpdateLayer(agentID, layerID, "line one\nline changed\nline three\n")
	if err != nil {
		t.Fatalf("UpdateLayer() v2 error = %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("v2 version = %d, want 2", v2.Version)
	}

	history, err := store.ListHistory(agentID, layerID)
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].Version != 1 || history[1].Version != 2 {
		t.Fatalf("unexpected history versions: %#v", []int{history[0].Version, history[1].Version})
	}

	gotV1, err := store.GetVersion(agentID, layerID, 1)
	if err != nil {
		t.Fatalf("GetVersion(v1) error = %v", err)
	}
	if gotV1.Content != "line one\nline two\n" {
		t.Fatalf("GetVersion(v1) content = %q, want original", gotV1.Content)
	}

	diff, err := store.DiffVersions(agentID, layerID, 1, 2)
	if err != nil {
		t.Fatalf("DiffVersions() error = %v", err)
	}
	if !strings.Contains(diff.UnifiedDiff, "-line two") {
		t.Fatalf("expected removed line in diff, got:\n%s", diff.UnifiedDiff)
	}
	if !strings.Contains(diff.UnifiedDiff, "+line changed") {
		t.Fatalf("expected added changed line in diff, got:\n%s", diff.UnifiedDiff)
	}
	if !strings.Contains(diff.UnifiedDiff, "+line three") {
		t.Fatalf("expected added line three in diff, got:\n%s", diff.UnifiedDiff)
	}

	rolledBack, err := store.Rollback(agentID, layerID, 1)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if rolledBack.Version != 3 {
		t.Fatalf("rollback version = %d, want 3", rolledBack.Version)
	}
	if rolledBack.Content != "line one\nline two\n" {
		t.Fatalf("rollback content = %q, want v1 content", rolledBack.Content)
	}

	current, err := store.GetCurrent(agentID)
	if err != nil {
		t.Fatalf("GetCurrent() error = %v", err)
	}
	if current.ToolSafetyRules.Version != 3 {
		t.Fatalf("current layer version = %d, want 3", current.ToolSafetyRules.Version)
	}
	if current.ToolSafetyRules.Content != "line one\nline two\n" {
		t.Fatalf("current layer content = %q, want rolled back content", current.ToolSafetyRules.Content)
	}

	historyAfterRollback, err := store.ListHistory(agentID, layerID)
	if err != nil {
		t.Fatalf("ListHistory(after rollback) error = %v", err)
	}
	if len(historyAfterRollback) != 3 {
		t.Fatalf("history length after rollback = %d, want 3", len(historyAfterRollback))
	}
	if historyAfterRollback[2].Version != 3 {
		t.Fatalf("last history version = %d, want 3", historyAfterRollback[2].Version)
	}
}

func TestVersionStorePersistsToDiskAcrossInstances(t *testing.T) {
	t.Parallel()

	controlPlane := filepath.Join(t.TempDir(), ".openclawssy")
	store, err := NewVersionStore(controlPlane)
	if err != nil {
		t.Fatalf("NewVersionStore() error = %v", err)
	}

	_, err = store.UpdateLayer("alpha", LayerAgentIdentity, "identity v1")
	if err != nil {
		t.Fatalf("UpdateLayer() error = %v", err)
	}

	store2, err := NewVersionStore(controlPlane)
	if err != nil {
		t.Fatalf("NewVersionStore(second) error = %v", err)
	}

	current, err := store2.GetCurrent("alpha")
	if err != nil {
		t.Fatalf("GetCurrent(second store) error = %v", err)
	}

	if current.AgentIdentity.Content != "identity v1" {
		t.Fatalf("persisted content = %q, want %q", current.AgentIdentity.Content, "identity v1")
	}
	if current.AgentIdentity.Version != 1 {
		t.Fatalf("persisted version = %d, want 1", current.AgentIdentity.Version)
	}
}
