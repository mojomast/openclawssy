package eval

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBaselineManagerSaveAndCompareLatestDetectsRegressions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, ".openclawssy", "eval", "results.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	baselineTS := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	latestTS := baselineTS.Add(5 * time.Minute)

	if _, err := store.SaveRun(context.Background(), SuiteRun{
		Suite:     "basic",
		Timestamp: baselineTS,
		Identity:  RunIdentity{InstanceID: "lab", AgentID: "default", RunID: "baseline-run"},
		Results: []CaseResult{
			{Name: "case-a", Result: TestResult{Passed: true, Expected: "hello", Actual: "hello tokens=3", DurationMS: 5}},
			{Name: "case-b", Result: TestResult{Passed: true, Expected: "delegate:no", Actual: "delegate:no tokens=2", DurationMS: 6}},
		},
		Metrics: Metrics{CompletionRate: 1, TokenCost: 5, TimeToCompletion: 11},
	}); err != nil {
		t.Fatalf("SaveRun(baseline source) error = %v", err)
	}

	manager, err := NewBaselineManager(store, root)
	if err != nil {
		t.Fatalf("NewBaselineManager() error = %v", err)
	}

	if err := manager.SaveBaseline(context.Background(), "basic"); err != nil {
		t.Fatalf("SaveBaseline() error = %v", err)
	}

	if _, err := store.SaveRun(context.Background(), SuiteRun{
		Suite:     "basic",
		Timestamp: latestTS,
		Results: []CaseResult{
			{Name: "case-a", Result: TestResult{Passed: false, Expected: "hello", Actual: "oops tokens=4", DurationMS: 7, Error: "mismatch"}},
			{Name: "case-b", Result: TestResult{Passed: true, Expected: "delegate:no", Actual: "delegate:no tokens=3", DurationMS: 8}},
		},
		Metrics: Metrics{CompletionRate: 0.5, TokenCost: 7, TimeToCompletion: 15},
	}); err != nil {
		t.Fatalf("SaveRun(latest) error = %v", err)
	}

	comparison, err := manager.CompareLatest(context.Background(), "basic")
	if err != nil {
		t.Fatalf("CompareLatest() error = %v", err)
	}
	if len(comparison.Regressions) != 1 {
		t.Fatalf("regression count = %d, want 1", len(comparison.Regressions))
	}
	if comparison.Regressions[0].TestName != "case-a" {
		t.Fatalf("regression test name = %q, want %q", comparison.Regressions[0].TestName, "case-a")
	}
	if comparison.Baseline.Timestamp != baselineTS {
		t.Fatalf("baseline timestamp = %s, want %s", comparison.Baseline.Timestamp, baselineTS)
	}
	if comparison.Baseline.Identity.RunID != "baseline-run" {
		t.Fatalf("expected baseline identity preserved, got %+v", comparison.Baseline.Identity)
	}
	if comparison.Latest.Timestamp != latestTS {
		t.Fatalf("latest timestamp = %s, want %s", comparison.Latest.Timestamp, latestTS)
	}
}

func TestBaselineManagerCompareWithoutBaselineFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, ".openclawssy", "eval", "results.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	manager, err := NewBaselineManager(store, root)
	if err != nil {
		t.Fatalf("NewBaselineManager() error = %v", err)
	}

	_, err = manager.CompareLatest(context.Background(), "basic")
	if !errors.Is(err, ErrBaselineNotFound) {
		t.Fatalf("CompareLatest() error = %v, want %v", err, ErrBaselineNotFound)
	}
}
