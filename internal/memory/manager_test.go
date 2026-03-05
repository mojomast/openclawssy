package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerWritesEventJSONL(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 8})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	ts := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	err = mgr.IngestEvent(context.Background(), Event{
		Type:      EventTypeUserMessage,
		Text:      "hello world",
		RunID:     "run_1",
		SessionID: "sess_1",
		Timestamp: ts,
		Metadata: map[string]any{
			"source": "dashboard",
		},
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}

	path := filepath.Join(agentsDir, "default", "memory", "events", "2026-02-18.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 event line, got %d", len(lines))
	}

	var got Event
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if got.ID == "" {
		t.Fatal("expected event id to be assigned")
	}
	if got.Type != EventTypeUserMessage {
		t.Fatalf("expected type %q, got %q", EventTypeUserMessage, got.Type)
	}
	if got.Text != "hello world" {
		t.Fatalf("expected text to roundtrip, got %q", got.Text)
	}
}

func TestManagerDropsWhenQueueFullWithoutBlocking(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 1})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	ctx := context.Background()
	var dropped bool
	for i := 0; i < 5000; i++ {
		err := mgr.IngestEvent(ctx, Event{Type: EventTypeToolCall, Text: "x"})
		if errors.Is(err, ErrQueueFull) {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatal("expected at least one queue-full drop")
	}
	if mgr.Stats().DroppedEvents == 0 {
		t.Fatal("expected dropped events stat to increase")
	}
}

func TestNewManagerRejectsInvalidAgentID(t *testing.T) {
	_, err := NewManager(t.TempDir(), "../escape", Options{Enabled: true})
	if !errors.Is(err, ErrInvalidAgentID) {
		t.Fatalf("expected ErrInvalidAgentID, got %v", err)
	}
}

// --- Buffer pressure tests ---

func TestManagerDropsTrackCountUnderBufferPressure(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 2})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	ctx := context.Background()
	totalDropped := uint64(0)
	for i := 0; i < 10000; i++ {
		err := mgr.IngestEvent(ctx, Event{Type: EventTypeToolCall, Text: "pressure"})
		if errors.Is(err, ErrQueueFull) {
			totalDropped++
		}
	}
	stats := mgr.Stats()
	if stats.DroppedEvents != totalDropped {
		t.Fatalf("expected DroppedEvents=%d, got %d", totalDropped, stats.DroppedEvents)
	}
	if totalDropped == 0 {
		t.Fatal("expected some drops under buffer pressure with BufferSize=2")
	}
}

func TestManagerBufferPressureDoesNotCrashRun(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 1})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Flood the queue — this must not panic or block.
	ctx := context.Background()
	for i := 0; i < 5000; i++ {
		_ = mgr.IngestEvent(ctx, Event{Type: EventTypeUserMessage, Text: "flood"})
	}

	// Close must complete without error (writer drains successfully).
	if err := mgr.Close(); err != nil {
		t.Fatalf("close after buffer pressure: %v", err)
	}
}

func TestManagerDisabledReturnsZeroStats(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), "default", Options{Enabled: false})
	if err != nil {
		t.Fatalf("new disabled manager: %v", err)
	}
	stats := mgr.Stats()
	if stats.DroppedEvents != 0 {
		t.Fatalf("expected 0 dropped events when disabled, got %d", stats.DroppedEvents)
	}
	// IngestEvent should be a no-op when disabled.
	if err := mgr.IngestEvent(context.Background(), Event{Type: EventTypeToolCall, Text: "x"}); err != nil {
		t.Fatalf("expected no error on disabled ingest, got %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("expected no error on disabled close, got %v", err)
	}
}

func TestManagerNilIsNoOp(t *testing.T) {
	var mgr *Manager
	// All methods must be safe on nil receiver.
	if err := mgr.IngestEvent(context.Background(), Event{Type: EventTypeToolCall}); err != nil {
		t.Fatalf("expected nil manager ingest to be no-op, got %v", err)
	}
	stats := mgr.Stats()
	if stats.DroppedEvents != 0 {
		t.Fatalf("expected 0 dropped events on nil manager, got %d", stats.DroppedEvents)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("expected nil manager close to be no-op, got %v", err)
	}
}

func TestManagerIngestAfterCloseReturnsError(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 8})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err = mgr.IngestEvent(context.Background(), Event{Type: EventTypeToolCall, Text: "after-close"})
	if !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed after close, got %v", err)
	}
}

// --- Checkpoint record tests ---

func TestCheckpointRecordRoundTripsFields(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	now := time.Now().UTC()
	record := CheckpointRecord{
		AgentID:          "default",
		CreatedAt:        now,
		FromTimestamp:    now.Add(-time.Hour),
		ToTimestamp:      now,
		EventCount:       42,
		NewItemCount:     3,
		UpdatedItemCount: 2,
		Summary:          "distilled 42 events",
	}
	_, err := WriteCheckpointRecord(agentsDir, "default", record)
	if err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	loaded, ok, err := LoadLatestCheckpointRecord(agentsDir, "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatal("expected checkpoint to exist")
	}
	if loaded.EventCount != 42 || loaded.NewItemCount != 3 || loaded.UpdatedItemCount != 2 {
		t.Fatalf("unexpected checkpoint: %+v", loaded)
	}
	if loaded.Summary != "distilled 42 events" {
		t.Fatalf("summary mismatch: %q", loaded.Summary)
	}
}

func TestLoadLatestCheckpointReturnsFalseWhenMissing(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	_, ok, err := LoadLatestCheckpointRecord(agentsDir, "default")
	if err != nil {
		t.Fatalf("load latest: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when no checkpoint exists")
	}
}

// --- Maintenance report tests ---

func TestMaintenanceReportRoundTrips(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	report := MaintenanceReport{
		AgentID:            "default",
		DeduplicatedCount:  2,
		ArchivedStaleCount: 1,
		VerificationCount:  3,
		Compacted:          true,
	}
	path, err := WriteMaintenanceReport(agentsDir, "default", report)
	if err != nil {
		t.Fatalf("write report: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("report file missing: %v", err)
	}
	// Verify the latest symlink-equivalent file also exists
	latestPath := filepath.Join(agentsDir, "default", "memory", "reports", "latest-maintenance.json")
	raw, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("read latest maintenance report: %v", err)
	}
	var loaded MaintenanceReport
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.DeduplicatedCount != 2 || loaded.ArchivedStaleCount != 1 {
		t.Fatalf("unexpected report: %+v", loaded)
	}
}

// --- NormalizeSearchParams and NormalizeItem tests ---

func TestNormalizeSearchParamsClampsValues(t *testing.T) {
	tests := []struct {
		name     string
		input    SearchParams
		wantLim  int
		wantMin  int
		wantStat string
	}{
		{"zero defaults", SearchParams{}, 8, 1, "active"},
		{"negative limit", SearchParams{Limit: -5}, 8, 1, "active"},
		{"over max limit", SearchParams{Limit: 100}, 50, 1, "active"},
		{"importance clamped high", SearchParams{MinImportance: 10}, 8, 5, "active"},
		{"importance clamped low", SearchParams{MinImportance: -1}, 8, 1, "active"},
		{"invalid status defaults", SearchParams{Status: "bogus"}, 8, 1, "active"},
		{"forgotten status preserved", SearchParams{Status: "forgotten"}, 8, 1, "forgotten"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSearchParams(tt.input)
			if got.Limit != tt.wantLim {
				t.Errorf("Limit: got %d, want %d", got.Limit, tt.wantLim)
			}
			if got.MinImportance != tt.wantMin {
				t.Errorf("MinImportance: got %d, want %d", got.MinImportance, tt.wantMin)
			}
			if got.Status != tt.wantStat {
				t.Errorf("Status: got %q, want %q", got.Status, tt.wantStat)
			}
		})
	}
}

func TestNormalizeItemAppliesDefaults(t *testing.T) {
	item := NormalizeItem(MemoryItem{})
	if item.Kind != "note" {
		t.Errorf("expected default kind 'note', got %q", item.Kind)
	}
	if item.Confidence != 0.7 {
		t.Errorf("expected default confidence 0.7, got %f", item.Confidence)
	}
	if item.Importance != 1 {
		t.Errorf("expected default importance 1, got %d", item.Importance)
	}
	if item.Status != "active" {
		t.Errorf("expected default status 'active', got %q", item.Status)
	}
}

func TestNormalizeItemClampsValues(t *testing.T) {
	item := NormalizeItem(MemoryItem{
		Importance: 10,
		Confidence: 2.0,
	})
	if item.Importance != 5 {
		t.Errorf("expected importance clamped to 5, got %d", item.Importance)
	}
	if item.Confidence != 1 {
		t.Errorf("expected confidence clamped to 1.0, got %f", item.Confidence)
	}
}

func TestAppendEventReadEventsAndCheckpointRecord(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	now := time.Now().UTC()
	if err := AppendEvent(agentsDir, "default", Event{Type: EventTypeDecisionLog, Text: "keep retries bounded", Timestamp: now}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := AppendEvent(agentsDir, "default", Event{Type: EventTypeError, Text: "timeout", Timestamp: now.Add(time.Second)}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	events, err := ReadEventsSince(agentsDir, "default", now.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	record := CheckpointRecord{AgentID: "default", CreatedAt: now.Add(2 * time.Second), EventCount: len(events)}
	checkpointPath, err := WriteCheckpointRecord(agentsDir, "default", record)
	if err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	if _, err := os.Stat(checkpointPath); err != nil {
		t.Fatalf("expected checkpoint file, got %v", err)
	}
	loaded, ok, err := LoadLatestCheckpointRecord(agentsDir, "default")
	if err != nil {
		t.Fatalf("load latest checkpoint: %v", err)
	}
	if !ok {
		t.Fatal("expected latest checkpoint to exist")
	}
	if loaded.EventCount != 2 {
		t.Fatalf("expected event_count=2, got %d", loaded.EventCount)
	}
}
