package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpchannel "openclawssy/internal/channels/http"
)

func TestRunDecisionsEndpointReturnsNestedChronologicalRecords(t *testing.T) {
	root := t.TempDir()
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{
		ID:        "run-parent",
		AgentID:   "default",
		Message:   "parent",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create parent run: %v", err)
	}

	now := time.Now().UTC()
	writeDecisionAuditEvent(t, root, "default", decisionAuditEvent{
		Timestamp: now.Add(2 * time.Second),
		Type:      "decision.record",
		RunID:     "run-parent",
		AgentID:   "default",
		Payload: map[string]any{
			"record_type":   "termination",
			"human_summary": "parent done",
			"payload":       map[string]any{"cause": "completed"},
		},
	})
	writeDecisionAuditEvent(t, root, "default", decisionAuditEvent{
		Timestamp: now,
		Type:      "decision.record",
		RunID:     "run-parent",
		AgentID:   "default",
		Payload: map[string]any{
			"record_type":   "goal_interpretation",
			"human_summary": "parent goal",
			"payload":       map[string]any{"goal": "ship feature"},
		},
	})
	writeDecisionAuditEvent(t, root, "worker", decisionAuditEvent{
		Timestamp: now.Add(1 * time.Second),
		Type:      "decision.record",
		RunID:     "run-child",
		AgentID:   "worker",
		Payload: map[string]any{
			"record_type":   "goal_interpretation",
			"human_summary": "child goal",
			"parent_run_id": "run-parent",
			"payload":       map[string]any{"goal": "implement patch", "parent_run_id": "run-parent"},
		},
	})

	h := New(root, store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/runs/run-parent/decisions", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		RunID   string `json:"run_id"`
		AgentID string `json:"agent_id"`
		Records []struct {
			RecordType string         `json:"record_type"`
			Payload    map[string]any `json:"payload"`
		} `json:"records"`
		Subagents []struct {
			RunID   string `json:"run_id"`
			AgentID string `json:"agent_id"`
			Records []struct {
				RecordType string         `json:"record_type"`
				Payload    map[string]any `json:"payload"`
			} `json:"records"`
		} `json:"subagents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.RunID != "run-parent" || payload.AgentID != "default" {
		t.Fatalf("unexpected root node: %#v", payload)
	}
	if len(payload.Records) != 2 {
		t.Fatalf("expected 2 parent records, got %#v", payload.Records)
	}
	if payload.Records[0].RecordType != "goal_interpretation" || payload.Records[1].RecordType != "termination" {
		t.Fatalf("expected chronological parent records, got %#v", payload.Records)
	}
	if len(payload.Subagents) != 1 {
		t.Fatalf("expected one nested subagent node, got %#v", payload.Subagents)
	}
	if payload.Subagents[0].RunID != "run-child" || payload.Subagents[0].AgentID != "worker" {
		t.Fatalf("unexpected child node: %#v", payload.Subagents[0])
	}
	if len(payload.Subagents[0].Records) != 1 {
		t.Fatalf("expected one child record, got %#v", payload.Subagents[0].Records)
	}
	if got := payload.Subagents[0].Records[0].Payload["parent_run_id"]; got != "run-parent" {
		t.Fatalf("expected nested payload to retain parent_run_id, got %#v", got)
	}
}

func TestRunDecisionsEndpointReturns404ForUnknownRun(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/runs/missing-run/decisions", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestRunDecisionsEndpointHandlesLargeAuditJSONLine(t *testing.T) {
	root := t.TempDir()
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{
		ID:        "run-large",
		AgentID:   "default",
		Message:   "large",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	now := time.Now().UTC()
	writeDecisionAuditEvent(t, root, "default", decisionAuditEvent{
		Timestamp: now,
		Type:      "decision.record",
		RunID:     "run-large",
		AgentID:   "default",
		Payload: map[string]any{
			"record_type":   "goal_interpretation",
			"human_summary": "small line",
			"payload":       map[string]any{"goal": "start"},
		},
	})
	writeDecisionAuditEvent(t, root, "default", decisionAuditEvent{
		Timestamp: now.Add(time.Second),
		Type:      "decision.record",
		RunID:     "run-large",
		AgentID:   "default",
		Payload: map[string]any{
			"record_type":   "strategy_selection",
			"human_summary": "large line",
			"payload": map[string]any{
				"details": strings.Repeat("x", 256*1024),
			},
		},
	})

	h := New(root, store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/runs/run-large/decisions", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		RunID   string `json:"run_id"`
		Records []struct {
			RecordType string `json:"record_type"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.RunID != "run-large" {
		t.Fatalf("expected run-large, got %q", payload.RunID)
	}
	if len(payload.Records) != 2 {
		t.Fatalf("expected 2 records, got %#v", payload.Records)
	}
	if payload.Records[0].RecordType != "goal_interpretation" || payload.Records[1].RecordType != "strategy_selection" {
		t.Fatalf("expected chronological records with large line parsed, got %#v", payload.Records)
	}
}

func writeDecisionAuditEvent(t *testing.T, root, agentID string, event decisionAuditEvent) {
	t.Helper()
	auditPath := filepath.Join(root, ".openclawssy", "agents", agentID, "audit", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal decision event: %v", err)
	}
	line = append(line, '\n')
	file, err := os.OpenFile(auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		t.Fatalf("write audit event: %v", err)
	}
}
