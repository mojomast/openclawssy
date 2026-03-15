package eval

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCreatesTableAndPersistsRuns(t *testing.T) {
	t.Parallel()

	store, err := OpenStore(filepath.Join(t.TempDir(), "results.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	var tableName string
	if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='eval_results'`).Scan(&tableName); err != nil {
		t.Fatalf("eval_results table lookup failed: %v", err)
	}
	if tableName != "eval_results" {
		t.Fatalf("table name = %q, want %q", tableName, "eval_results")
	}

	firstTS := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	secondTS := firstTS.Add(2 * time.Minute)

	firstID, err := store.SaveRun(context.Background(), SuiteRun{
		Suite:     "basic",
		Timestamp: firstTS,
		Results:   []CaseResult{{Name: "case-1", Result: TestResult{Passed: true, Expected: "hello", Actual: "hello tokens=2", DurationMS: 5}}},
		Metrics:   Metrics{CompletionRate: 1, TokenCost: 2, TimeToCompletion: 5},
	})
	if err != nil {
		t.Fatalf("SaveRun(first) error = %v", err)
	}

	secondID, err := store.SaveRun(context.Background(), SuiteRun{
		Suite:     "basic",
		Timestamp: secondTS,
		Results:   []CaseResult{{Name: "case-1", Result: TestResult{Passed: false, Expected: "hello", Actual: "nope tokens=3", DurationMS: 6, Error: "mismatch"}}},
		Metrics:   Metrics{CompletionRate: 0, TokenCost: 3, TimeToCompletion: 6},
	})
	if err != nil {
		t.Fatalf("SaveRun(second) error = %v", err)
	}
	if secondID <= firstID {
		t.Fatalf("run IDs not increasing: first=%d second=%d", firstID, secondID)
	}

	runs, err := store.ListRuns(context.Background(), "basic", 10)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListRuns() length = %d, want 2", len(runs))
	}
	if runs[0].Timestamp.Before(runs[1].Timestamp) {
		t.Fatalf("runs expected newest-first ordering: %#v", runs)
	}

	latest, ok, err := store.LatestRun(context.Background(), "basic")
	if err != nil {
		t.Fatalf("LatestRun() error = %v", err)
	}
	if !ok {
		t.Fatal("LatestRun() found = false, want true")
	}
	if latest.ID != secondID {
		t.Fatalf("latest ID = %d, want %d", latest.ID, secondID)
	}
	if latest.Results[0].Result.Passed {
		t.Fatal("latest result should be failing case")
	}
}

func TestStoreLatestRunNotFound(t *testing.T) {
	t.Parallel()

	store, err := OpenStore(filepath.Join(t.TempDir(), "results.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	_, ok, err := store.LatestRun(context.Background(), "missing")
	if err != nil {
		t.Fatalf("LatestRun() error = %v", err)
	}
	if ok {
		t.Fatal("LatestRun() found = true, want false")
	}

	if _, err := OpenStore(" "); !errors.Is(err, ErrInvalidStorePath) {
		t.Fatalf("OpenStore(empty) error = %v, want %v", err, ErrInvalidStorePath)
	}
}
