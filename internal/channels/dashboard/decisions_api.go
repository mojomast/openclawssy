package dashboard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/audit"
	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/instances"
)

type runDecisionNode struct {
	RunID      string                 `json:"run_id"`
	InstanceID string                 `json:"instance_id,omitempty"`
	AgentID    string                 `json:"agent_id,omitempty"`
	Records    []agent.DecisionRecord `json:"records"`
	Subagents  []runDecisionNode      `json:"subagents,omitempty"`
}

type decisionAuditEvent struct {
	Timestamp time.Time      `json:"ts"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	AgentID   string         `json:"agent_id"`
	Payload   map[string]any `json:"payload"`
}

func (h *Handler) handleRunDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimPrefix(r.URL.Path, "/api/admin/runs/")
	if suffix == r.URL.Path || !strings.HasSuffix(suffix, "/decisions") {
		http.NotFound(w, r)
		return
	}
	runID := strings.TrimSpace(strings.TrimSuffix(suffix, "/decisions"))
	if runID == "" || strings.Contains(runID, "/") {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	run, err := h.store.Get(r.Context(), runID)
	if err != nil {
		if errors.Is(err, httpchannel.ErrRunNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load run", http.StatusInternalServerError)
		return
	}

	instanceID := normalizeDashboardInstanceID(run.InstanceID)
	recordsByRun, parentByRun, agentByRun, instanceByRun, err := h.loadDecisionRecords(instanceID)
	if err != nil {
		http.Error(w, "failed to load decision records", http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(agentByRun[runID]) == "" {
		agentByRun[runID] = strings.TrimSpace(run.AgentID)
	}
	if strings.TrimSpace(instanceByRun[runID]) == "" {
		instanceByRun[runID] = instanceID
	}

	node := buildRunDecisionTree(runID, recordsByRun, parentByRun, agentByRun, instanceByRun)
	writeJSON(w, node)
}

func (h *Handler) loadDecisionRecords(instanceID string) (map[string][]agent.DecisionRecord, map[string]string, map[string]string, map[string]string, error) {
	recordsByRun := make(map[string][]agent.DecisionRecord)
	parentByRun := make(map[string]string)
	agentByRun := make(map[string]string)
	instanceByRun := make(map[string]string)

	agentsRoot, err := instances.AgentsDir(h.rootDir, instanceID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	entries, err := os.ReadDir(agentsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if instanceID == instances.DefaultInstanceID {
				legacyAgentsRoot := filepath.Join(h.rootDir, ".openclawssy", "agents")
				legacyEntries, legacyErr := os.ReadDir(legacyAgentsRoot)
				if legacyErr != nil {
					if errors.Is(legacyErr, os.ErrNotExist) {
						return recordsByRun, parentByRun, agentByRun, instanceByRun, nil
					}
					return nil, nil, nil, nil, legacyErr
				}
				for _, entry := range legacyEntries {
					if !entry.IsDir() {
						continue
					}
					auditPath := filepath.Join(legacyAgentsRoot, entry.Name(), "audit", "events.jsonl")
					if err := readDecisionAuditFile(instanceID, auditPath, recordsByRun, parentByRun, agentByRun, instanceByRun); err != nil {
						if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
							continue
						}
						return nil, nil, nil, nil, err
					}
				}
				return recordsByRun, parentByRun, agentByRun, instanceByRun, nil
			}
			return recordsByRun, parentByRun, agentByRun, instanceByRun, nil
		}
		return nil, nil, nil, nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		auditPath := filepath.Join(agentsRoot, entry.Name(), "audit", "events.jsonl")
		if err := readDecisionAuditFile(instanceID, auditPath, recordsByRun, parentByRun, agentByRun, instanceByRun); err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			return nil, nil, nil, nil, err
		}
	}

	for runID := range recordsByRun {
		sort.SliceStable(recordsByRun[runID], func(i, j int) bool {
			left := recordsByRun[runID][i]
			right := recordsByRun[runID][j]
			if left.Timestamp.Equal(right.Timestamp) {
				if left.RecordType == right.RecordType {
					return left.HumanSummary < right.HumanSummary
				}
				return left.RecordType < right.RecordType
			}
			return left.Timestamp.Before(right.Timestamp)
		})
	}

	return recordsByRun, parentByRun, agentByRun, instanceByRun, nil
}

func readDecisionAuditFile(instanceID string, path string, recordsByRun map[string][]agent.DecisionRecord, parentByRun map[string]string, agentByRun map[string]string, instanceByRun map[string]string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}

		var event decisionAuditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if strings.TrimSpace(event.Type) != audit.EventDecisionRecord {
			continue
		}

		runID := strings.TrimSpace(event.RunID)
		agentID := strings.TrimSpace(event.AgentID)
		recordType := stringField(event.Payload, "record_type")
		humanSummary := stringField(event.Payload, "human_summary")
		payload := mapField(event.Payload, "payload")
		if payload == nil {
			payload = map[string]any{}
		}

		parentRunID := strings.TrimSpace(stringField(event.Payload, "parent_run_id"))
		if parentRunID == "" {
			parentRunID = strings.TrimSpace(stringField(payload, "parent_run_id"))
		}
		if parentRunID != "" {
			if _, ok := payload["parent_run_id"]; !ok {
				payload["parent_run_id"] = parentRunID
			}
			if runID != "" {
				parentByRun[runID] = parentRunID
			}
		}

		timestamp := event.Timestamp
		if timestamp.IsZero() {
			timestamp = time.Now().UTC()
		}
		record := agent.DecisionRecord{
			Timestamp:    timestamp.UTC(),
			RunID:        runID,
			AgentID:      agentID,
			RecordType:   recordType,
			Payload:      payload,
			HumanSummary: humanSummary,
		}
		recordsByRun[runID] = append(recordsByRun[runID], record)
		if runID != "" && agentID != "" {
			agentByRun[runID] = agentID
			instanceByRun[runID] = normalizeDashboardInstanceID(instanceID)
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	return nil
}

func buildRunDecisionTree(rootRunID string, recordsByRun map[string][]agent.DecisionRecord, parentByRun map[string]string, agentByRun map[string]string, instanceByRun map[string]string) runDecisionNode {
	childrenByParent := make(map[string][]string)
	for runID, parentRunID := range parentByRun {
		runID = strings.TrimSpace(runID)
		parentRunID = strings.TrimSpace(parentRunID)
		if runID == "" || parentRunID == "" || runID == parentRunID {
			continue
		}
		childrenByParent[parentRunID] = append(childrenByParent[parentRunID], runID)
	}
	for parentRunID := range childrenByParent {
		sort.Strings(childrenByParent[parentRunID])
	}

	seen := make(map[string]struct{})
	var build func(runID string) runDecisionNode
	build = func(runID string) runDecisionNode {
		seen[runID] = struct{}{}
		node := runDecisionNode{
			RunID:      runID,
			InstanceID: normalizeDashboardInstanceID(instanceByRun[runID]),
			AgentID:    strings.TrimSpace(agentByRun[runID]),
			Records:    append([]agent.DecisionRecord(nil), recordsByRun[runID]...),
		}
		for _, childRunID := range childrenByParent[runID] {
			if _, ok := seen[childRunID]; ok {
				continue
			}
			node.Subagents = append(node.Subagents, build(childRunID))
		}
		return node
	}

	return build(strings.TrimSpace(rootRunID))
}

func stringField(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func mapField(values map[string]any, key string) map[string]any {
	if len(values) == 0 {
		return nil
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return nil
	}
	payload, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	cloned := make(map[string]any, len(payload))
	for k, v := range payload {
		cloned[k] = v
	}
	return cloned
}
