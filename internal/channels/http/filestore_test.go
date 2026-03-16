package httpchannel

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRunStorePersistsRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.json")
	store, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	run := Run{ID: "run-1", AgentID: "a", Message: "m", Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if _, err := store.Create(context.Background(), run); err != nil {
		t.Fatalf("create: %v", err)
	}

	reloaded, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.Get(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get reloaded run: %v", err)
	}
	if got.ID != run.ID || got.AgentID != run.AgentID {
		t.Fatalf("unexpected run: %+v", got)
	}
}

func TestFileRunStorePersistsInstanceID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.json")
	store, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	run := Run{ID: "run-instance", InstanceID: "instance-a", AgentID: "a", Message: "m", Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if _, err := store.Create(context.Background(), run); err != nil {
		t.Fatalf("create: %v", err)
	}

	reloaded, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.Get(context.Background(), "run-instance")
	if err != nil {
		t.Fatalf("get reloaded run: %v", err)
	}
	if got.InstanceID != "instance-a" {
		t.Fatalf("expected persisted instance_id instance-a, got %+v", got)
	}
}

func TestFileRunStoreDerivesDecompositionPlanFromTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.json")
	store, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	run := Run{
		ID:        "run-plan",
		AgentID:   "a",
		Message:   "m",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Trace: map[string]any{
			"decomposition_plan": map[string]any{
				"delegation_mode": "suggest_only",
				"tasks":           []any{map[string]any{"task_id": "discover"}},
			},
		},
	}
	if _, err := store.Create(context.Background(), run); err != nil {
		t.Fatalf("create: %v", err)
	}

	reloaded, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.Get(context.Background(), "run-plan")
	if err != nil {
		t.Fatalf("get reloaded run: %v", err)
	}
	if got.DecompositionPlan == nil {
		t.Fatalf("expected derived decomposition_plan, got %+v", got)
	}
}

func TestFileRunStoreCompactsOldTerminalRuns(t *testing.T) {
	prevMax := fileRunStoreMaxPersistedRuns
	fileRunStoreMaxPersistedRuns = 5
	defer func() { fileRunStoreMaxPersistedRuns = prevMax }()

	path := filepath.Join(t.TempDir(), "runs.json")
	store, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 8; i++ {
		run := Run{
			ID:        fmt.Sprintf("run-term-%d", i),
			AgentID:   "a",
			Message:   "m",
			Status:    "completed",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
			UpdatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if _, err := store.Create(context.Background(), run); err != nil {
			t.Fatalf("create terminal run %d: %v", i, err)
		}
	}

	running := Run{ID: "run-running", AgentID: "a", Message: "m", Status: "running", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if _, err := store.Create(context.Background(), running); err != nil {
		t.Fatalf("create running run: %v", err)
	}

	runs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 5 {
		t.Fatalf("expected compacted 5 runs, got %d", len(runs))
	}

	if _, err := store.Get(context.Background(), "run-term-0"); err == nil {
		t.Fatal("expected oldest terminal run to be compacted out")
	}
	if _, err := store.Get(context.Background(), "run-running"); err != nil {
		t.Fatalf("expected running run preserved, got %v", err)
	}
}
