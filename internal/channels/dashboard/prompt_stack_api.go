package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"openclawssy/internal/promptstack"
)

var (
	promptStackToolNamePattern     = regexp.MustCompile(`\b[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+\b`)
	promptStackTerminationPatternA = regexp.MustCompile(`\b(stop|return|terminate|halt|end)\b[^\n\.]{0,80}\b(when|once|if|upon)\b`)
	promptStackTerminationPatternB = regexp.MustCompile(`\b(when|once|if|upon)\b[^\n\.]{0,80}\b(stop|return|terminate|halt|end)\b`)
)

type promptStackPreviewLayer struct {
	LayerID    string `json:"layer_id"`
	Content    string `json:"content"`
	Version    int    `json:"version"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	WordCount  int    `json:"word_count"`
	TokenCount int    `json:"token_count"`
}

type promptStackSnapshot struct {
	Version       int            `json:"version"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
	ChangedLayer  string         `json:"changed_layer"`
	LayerVersion  int            `json:"layer_version"`
	LayerVersions map[string]int `json:"layer_versions"`
}

type promptStackSnapshotState struct {
	Meta  promptStackSnapshot
	Stack promptstack.PromptStack
}

type promptStackHistoryEvent struct {
	Layer promptstack.PromptLayer
}

type promptStackStructuralCheck struct {
	Name        string `json:"name"`
	Passed      bool   `json:"passed"`
	Explanation string `json:"explanation"`
}

func (h *Handler) handlePromptStackAPI(w http.ResponseWriter, r *http.Request, agentID string, segments []string) {
	store, err := h.promptStackStore()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "promptstack.store_init_failed", "failed to initialize prompt stack store", nil)
		return
	}

	if len(segments) == 0 {
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.getPromptStack(w, agentID, store)
		return
	}

	if len(segments) != 1 {
		http.NotFound(w, r)
		return
	}

	target := strings.TrimSpace(segments[0])
	switch target {
	case "preview":
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.getPromptStackPreview(w, agentID, store)
	case "history":
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.getPromptStackHistory(w, agentID, store)
	case "diff":
		if r.Method != http.MethodGet {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.getPromptStackDiff(w, r, agentID, store)
	case "rollback":
		if r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.rollbackPromptStack(w, r, agentID, store)
	case "lint":
		if r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.lintPromptStack(w, agentID, store)
	case "test":
		if r.Method != http.MethodPost {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.testPromptStack(w, agentID, store)
	default:
		if r.Method != http.MethodPut {
			writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
			return
		}
		h.updatePromptStackLayer(w, r, agentID, target, store)
	}
}

func (h *Handler) promptStackStore() (*promptstack.VersionStore, error) {
	return promptstack.NewVersionStore(filepath.Join(h.rootDir, ".openclawssy"))
}

func (h *Handler) getPromptStack(w http.ResponseWriter, agentID string, store *promptstack.VersionStore) {
	stack, err := h.ensurePromptStackInitialized(agentID, store)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	writeJSON(w, map[string]any{
		"agent_id": agentID,
		"layers":   stack.LayersInOrder(),
	})
}

func (h *Handler) updatePromptStackLayer(w http.ResponseWriter, r *http.Request, agentID, layerID string, store *promptstack.VersionStore) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "promptstack.invalid_json", "invalid json body", nil)
		return
	}

	if _, err := h.ensurePromptStackInitialized(agentID, store); err != nil {
		h.writePromptStackError(w, err)
		return
	}

	updated, err := store.UpdateLayer(agentID, layerID, req.Content)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	stack, err := store.GetCurrent(agentID)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	writeJSON(w, map[string]any{
		"ok":            true,
		"updated_layer": updated,
		"layers":        stack.LayersInOrder(),
	})
}

func (h *Handler) getPromptStackPreview(w http.ResponseWriter, agentID string, store *promptstack.VersionStore) {
	stack, err := h.ensurePromptStackInitialized(agentID, store)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	estimate := promptstack.EstimateTokens(stack)
	byLayer := make(map[string]promptstack.LayerTokenEstimate, len(estimate.PerLayer))
	for _, item := range estimate.PerLayer {
		byLayer[item.LayerID] = item
	}

	previewLayers := make([]promptStackPreviewLayer, 0, len(promptstack.LayerIDs()))
	for _, layer := range stack.LayersInOrder() {
		tokens := byLayer[layer.LayerID]
		previewLayers = append(previewLayers, promptStackPreviewLayer{
			LayerID:    layer.LayerID,
			Content:    layer.Content,
			Version:    layer.Version,
			UpdatedAt:  promptStackTime(layer.UpdatedAt),
			WordCount:  tokens.WordCount,
			TokenCount: tokens.TokenCount,
		})
	}

	writeJSON(w, map[string]any{
		"agent_id":          agentID,
		"layers":            previewLayers,
		"assembled_prompt":  promptstack.Assemble(stack),
		"total_tokens":      estimate.TotalTokens,
		"estimation_method": estimate.EstimationMethod,
	})
}

func (h *Handler) getPromptStackHistory(w http.ResponseWriter, agentID string, store *promptstack.VersionStore) {
	if _, err := h.ensurePromptStackInitialized(agentID, store); err != nil {
		h.writePromptStackError(w, err)
		return
	}

	layerHistory, err := store.ListHistoryByLayer(agentID)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}
	timeline := buildPromptStackTimeline(layerHistory)
	versions := make([]promptStackSnapshot, 0, len(timeline))
	for _, snapshot := range timeline {
		versions = append(versions, snapshot.Meta)
	}

	writeJSON(w, map[string]any{
		"agent_id": agentID,
		"layers":   layerHistory,
		"versions": versions,
		"count":    len(versions),
	})
}

func (h *Handler) getPromptStackDiff(w http.ResponseWriter, r *http.Request, agentID string, store *promptstack.VersionStore) {
	if _, err := h.ensurePromptStackInitialized(agentID, store); err != nil {
		h.writePromptStackError(w, err)
		return
	}

	fromVersion, err := parsePromptStackVersion(r.URL.Query().Get("v1"))
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "promptstack.invalid_version", "v1 must be a positive integer", nil)
		return
	}
	toVersion, err := parsePromptStackVersion(r.URL.Query().Get("v2"))
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "promptstack.invalid_version", "v2 must be a positive integer", nil)
		return
	}

	layerHistory, err := store.ListHistoryByLayer(agentID)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}
	timeline := buildPromptStackTimeline(layerHistory)
	fromSnapshot, ok := findPromptStackSnapshot(timeline, fromVersion)
	if !ok {
		writeDashboardError(w, http.StatusNotFound, "promptstack.version_not_found", "v1 version not found", nil)
		return
	}
	toSnapshot, ok := findPromptStackSnapshot(timeline, toVersion)
	if !ok {
		writeDashboardError(w, http.StatusNotFound, "promptstack.version_not_found", "v2 version not found", nil)
		return
	}

	fromPrompt := promptstack.Assemble(fromSnapshot.Stack)
	toPrompt := promptstack.Assemble(toSnapshot.Stack)
	diff := buildPromptStackTextDiff(fromVersion, toVersion, fromPrompt, toPrompt)

	writeJSON(w, map[string]any{
		"agent_id":     agentID,
		"from_version": fromVersion,
		"to_version":   toVersion,
		"from_prompt":  fromPrompt,
		"to_prompt":    toPrompt,
		"diff":         diff,
	})
}

func (h *Handler) rollbackPromptStack(w http.ResponseWriter, r *http.Request, agentID string, store *promptstack.VersionStore) {
	var req struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "promptstack.invalid_json", "invalid json body", nil)
		return
	}
	if req.Version < 1 {
		writeDashboardError(w, http.StatusBadRequest, "promptstack.invalid_version", "version must be a positive integer", nil)
		return
	}

	if _, err := h.ensurePromptStackInitialized(agentID, store); err != nil {
		h.writePromptStackError(w, err)
		return
	}

	layerHistory, err := store.ListHistoryByLayer(agentID)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}
	timeline := buildPromptStackTimeline(layerHistory)
	targetSnapshot, ok := findPromptStackSnapshot(timeline, req.Version)
	if !ok {
		writeDashboardError(w, http.StatusNotFound, "promptstack.version_not_found", "requested version not found", nil)
		return
	}

	current, err := store.GetCurrent(agentID)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	for _, layerID := range promptstack.LayerIDs() {
		targetLayer, _ := targetSnapshot.Stack.Layer(layerID)
		currentLayer, _ := current.Layer(layerID)

		switch {
		case targetLayer.Version > 0:
			if _, err := store.Rollback(agentID, layerID, targetLayer.Version); err != nil {
				h.writePromptStackError(w, err)
				return
			}
		case currentLayer.Version > 0 || strings.TrimSpace(currentLayer.Content) != "":
			if _, err := store.UpdateLayer(agentID, layerID, ""); err != nil {
				h.writePromptStackError(w, err)
				return
			}
		}
	}

	stack, err := store.GetCurrent(agentID)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	writeJSON(w, map[string]any{
		"ok":      true,
		"version": req.Version,
		"layers":  stack.LayersInOrder(),
	})
}

func (h *Handler) lintPromptStack(w http.ResponseWriter, agentID string, store *promptstack.VersionStore) {
	stack, err := h.ensurePromptStackInitialized(agentID, store)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	issues := promptstack.Lint(stack, nil)
	writeJSON(w, map[string]any{
		"agent_id": agentID,
		"issues":   issues,
		"count":    len(issues),
	})
}

func (h *Handler) testPromptStack(w http.ResponseWriter, agentID string, store *promptstack.VersionStore) {
	stack, err := h.ensurePromptStackInitialized(agentID, store)
	if err != nil {
		h.writePromptStackError(w, err)
		return
	}

	checks := runPromptStackStructuralChecks(stack)
	passed := true
	for _, check := range checks {
		if !check.Passed {
			passed = false
			break
		}
	}

	writeJSON(w, map[string]any{
		"agent_id": agentID,
		"passed":   passed,
		"checks":   checks,
	})
}

func (h *Handler) ensurePromptStackInitialized(agentID string, store *promptstack.VersionStore) (promptstack.PromptStack, error) {
	stack, err := store.GetCurrent(agentID)
	if err != nil {
		return promptstack.PromptStack{}, err
	}
	if promptStackHasPersistedContent(stack) {
		return stack, nil
	}

	seed := h.seedPromptStackFromDocs(agentID)
	for _, layer := range seed.LayersInOrder() {
		if strings.TrimSpace(layer.Content) == "" {
			continue
		}
		if _, err := store.UpdateLayer(agentID, layer.LayerID, layer.Content); err != nil {
			return promptstack.PromptStack{}, err
		}
	}

	return store.GetCurrent(agentID)
}

func promptStackHasPersistedContent(stack promptstack.PromptStack) bool {
	for _, layer := range stack.LayersInOrder() {
		if layer.Version > 0 {
			return true
		}
		if strings.TrimSpace(layer.Content) != "" {
			return true
		}
	}
	return false
}

func (h *Handler) seedPromptStackFromDocs(agentID string) promptstack.PromptStack {
	stack := promptstack.NewPromptStack()

	layerContent := map[string]string{
		promptstack.LayerGlobalOperatorPolicy: h.readPromptStackDoc(agentID, "SPECPLAN.md"),
		promptstack.LayerAgentIdentity:        h.readPromptStackDoc(agentID, "SOUL.md"),
		promptstack.LayerToolSafetyRules: joinPromptStackSeedDocs(
			h.readPromptStackDoc(agentID, "RULES.md"),
			h.readPromptStackDoc(agentID, "TOOLS.md"),
		),
		promptstack.LayerDelegationPolicy: h.readPromptStackDoc(agentID, "DEVPLAN.md"),
		promptstack.LayerSessionOverlay: joinPromptStackSeedDocs(
			h.readPromptStackDoc(agentID, "HEARTBEAT.md"),
			h.readPromptStackDoc(agentID, "HANDOFF.md"),
		),
	}

	for _, layerID := range promptstack.LayerIDs() {
		_ = stack.SetLayer(promptstack.PromptLayer{
			LayerID: layerID,
			Content: layerContent[layerID],
		})
	}

	return stack
}

func (h *Handler) readPromptStackDoc(agentID, name string) string {
	doc, err := h.readAgentDoc(agentID, name)
	if err != nil || !doc.Exists {
		return ""
	}
	return strings.TrimSpace(doc.Content)
}

func joinPromptStackSeedDocs(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		nonEmpty = append(nonEmpty, trimmed)
	}
	return strings.Join(nonEmpty, "\n\n")
}

func (h *Handler) writePromptStackError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, promptstack.ErrInvalidLayerID):
		writeDashboardError(w, http.StatusBadRequest, "promptstack.invalid_layer", "invalid layer id", nil)
	case errors.Is(err, promptstack.ErrInvalidAgentID):
		writeDashboardError(w, http.StatusBadRequest, "agents.invalid_agent_id", "invalid agent id", nil)
	case errors.Is(err, promptstack.ErrVersionNotFound):
		writeDashboardError(w, http.StatusNotFound, "promptstack.version_not_found", "prompt stack version not found", nil)
	default:
		writeDashboardError(w, http.StatusInternalServerError, "promptstack.operation_failed", err.Error(), nil)
	}
}

func parsePromptStackVersion(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("version is required")
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("version must be a positive integer")
	}
	return parsed, nil
}

func buildPromptStackTimeline(layerHistory map[string][]promptstack.PromptLayer) []promptStackSnapshotState {
	events := make([]promptStackHistoryEvent, 0)
	for _, layerID := range promptstack.LayerIDs() {
		for _, layer := range layerHistory[layerID] {
			if layer.Version < 1 {
				continue
			}
			events = append(events, promptStackHistoryEvent{Layer: layer})
		}
	}

	sort.Slice(events, func(i, j int) bool {
		left := events[i].Layer
		right := events[j].Layer
		switch {
		case left.UpdatedAt.Before(right.UpdatedAt):
			return true
		case left.UpdatedAt.After(right.UpdatedAt):
			return false
		case left.LayerID != right.LayerID:
			return left.LayerID < right.LayerID
		default:
			return left.Version < right.Version
		}
	})

	current := promptstack.NewPromptStack()
	out := make([]promptStackSnapshotState, 0, len(events))
	for i, event := range events {
		_ = current.SetLayer(event.Layer)
		stackSnapshot := current
		layerVersions := make(map[string]int, len(promptstack.LayerIDs()))
		for _, layer := range stackSnapshot.LayersInOrder() {
			layerVersions[layer.LayerID] = layer.Version
		}

		out = append(out, promptStackSnapshotState{
			Meta: promptStackSnapshot{
				Version:       i + 1,
				UpdatedAt:     promptStackTime(event.Layer.UpdatedAt),
				ChangedLayer:  event.Layer.LayerID,
				LayerVersion:  event.Layer.Version,
				LayerVersions: layerVersions,
			},
			Stack: stackSnapshot,
		})
	}

	return out
}

func findPromptStackSnapshot(timeline []promptStackSnapshotState, version int) (promptStackSnapshotState, bool) {
	for _, snapshot := range timeline {
		if snapshot.Meta.Version == version {
			return snapshot, true
		}
	}
	return promptStackSnapshotState{}, false
}

func promptStackTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func buildPromptStackTextDiff(fromVersion, toVersion int, fromContent, toContent string) promptstack.VersionDiff {
	lines := diffPromptStackLines(fromContent, toContent)
	return promptstack.VersionDiff{
		LayerID:     promptstack.LintLayerAll,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Lines:       lines,
		UnifiedDiff: renderPromptStackUnifiedDiff(fromVersion, toVersion, lines),
	}
}

func diffPromptStackLines(fromContent, toContent string) []promptstack.DiffLine {
	fromLines := promptStackSplitLines(fromContent)
	toLines := promptStackSplitLines(toContent)

	n := len(fromLines)
	m := len(toLines)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if fromLines[i] == toLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	result := make([]promptstack.DiffLine, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case fromLines[i] == toLines[j]:
			result = append(result, promptstack.DiffLine{Type: promptstack.DiffLineUnchanged, Content: fromLines[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			result = append(result, promptstack.DiffLine{Type: promptstack.DiffLineRemoved, Content: fromLines[i]})
			i++
		default:
			result = append(result, promptstack.DiffLine{Type: promptstack.DiffLineAdded, Content: toLines[j]})
			j++
		}
	}

	for i < n {
		result = append(result, promptstack.DiffLine{Type: promptstack.DiffLineRemoved, Content: fromLines[i]})
		i++
	}
	for j < m {
		result = append(result, promptstack.DiffLine{Type: promptstack.DiffLineAdded, Content: toLines[j]})
		j++
	}

	return result
}

func renderPromptStackUnifiedDiff(fromVersion, toVersion int, lines []promptstack.DiffLine) string {
	var b strings.Builder
	b.WriteString("--- version ")
	b.WriteString(strconv.Itoa(fromVersion))
	b.WriteString("\n")
	b.WriteString("+++ version ")
	b.WriteString(strconv.Itoa(toVersion))
	b.WriteString("\n")

	for _, line := range lines {
		switch line.Type {
		case promptstack.DiffLineAdded:
			b.WriteString("+")
		case promptstack.DiffLineRemoved:
			b.WriteString("-")
		default:
			b.WriteString(" ")
		}
		b.WriteString(line.Content)
		b.WriteString("\n")
	}

	return b.String()
}

func promptStackSplitLines(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if normalized == "" {
		return nil
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func runPromptStackStructuralChecks(stack promptstack.PromptStack) []promptStackStructuralCheck {
	assembled := strings.ToLower(promptstack.Assemble(stack))

	checks := []promptStackStructuralCheck{
		{
			Name:        "termination_rules",
			Passed:      promptStackHasTerminationRules(assembled),
			Explanation: "Prompt includes explicit stop/return conditions.",
		},
		{
			Name:        "allowed_tools_mentioned",
			Passed:      promptStackMentionsAllowedTools(assembled),
			Explanation: "Prompt mentions allowed tools or tool allowlist directives.",
		},
		{
			Name:        "delegation_instructions_present",
			Passed:      promptStackHasDelegationInstructions(assembled),
			Explanation: "Prompt contains delegation instructions for subagent handoff/routing.",
		},
	}

	for i := range checks {
		if checks[i].Passed {
			checks[i].Explanation = "PASS: " + checks[i].Explanation
		} else {
			checks[i].Explanation = "FAIL: " + checks[i].Explanation
		}
	}

	return checks
}

func promptStackHasTerminationRules(content string) bool {
	terminationSignals := []string{
		"stop and return",
		"stop when",
		"return when",
		"once complete, return",
		"if blocked, return",
		"terminate when",
		"halt when",
		"end when",
	}
	for _, signal := range terminationSignals {
		if strings.Contains(content, signal) {
			return true
		}
	}
	return promptStackTerminationPatternA.MatchString(content) || promptStackTerminationPatternB.MatchString(content)
}

func promptStackMentionsAllowedTools(content string) bool {
	hasToolWord := strings.Contains(content, "tool")
	hasAllowSignal := strings.Contains(content, "allow") || strings.Contains(content, "permit") || strings.Contains(content, "approved")
	hasToolName := promptStackToolNamePattern.MatchString(content)
	return (hasToolWord && hasAllowSignal) || (hasAllowSignal && hasToolName)
}

func promptStackHasDelegationInstructions(content string) bool {
	return strings.Contains(content, "delegate") || strings.Contains(content, "delegation") || strings.Contains(content, "subagent")
}
