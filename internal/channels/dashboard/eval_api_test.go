package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/eval"
)

func TestEvalResultsEndpointReturnsHistoryMetricsAndRegressions(t *testing.T) {
	root := t.TempDir()
	store, err := eval.OpenStore(filepath.Join(root, ".openclawssy", "eval", "results.db"))
	if err != nil {
		t.Fatalf("open eval store: %v", err)
	}
	defer func() { _ = store.Close() }()

	baselineTimestamp := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	latestTimestamp := baselineTimestamp.Add(2 * time.Minute)

	_, err = store.SaveRun(context.Background(), eval.SuiteRun{
		Suite:     "basic",
		Timestamp: baselineTimestamp,
		Results: []eval.CaseResult{
			{Name: "case-pass", Result: eval.TestResult{Passed: true, Expected: "ok", Actual: "ok", DurationMS: 10}},
			{Name: "case-regression", Result: eval.TestResult{Passed: true, Expected: "stable", Actual: "stable", DurationMS: 12}},
		},
		Metrics: eval.Metrics{
			CompletionRate:      1,
			ToolMisuseRate:      0,
			DelegationPrecision: 1,
			TokenCost:           20,
			TimeToCompletion:    22,
		},
	})
	if err != nil {
		t.Fatalf("save baseline source run: %v", err)
	}

	manager, err := eval.NewBaselineManager(store, root)
	if err != nil {
		t.Fatalf("new baseline manager: %v", err)
	}
	if err := manager.SaveBaseline(context.Background(), "basic"); err != nil {
		t.Fatalf("save baseline: %v", err)
	}

	_, err = store.SaveRun(context.Background(), eval.SuiteRun{
		Suite:     "basic",
		Timestamp: latestTimestamp,
		Identity:  eval.RunIdentity{InstanceID: "lab", AgentID: "default", RunID: "eval-run-2", ParentRunID: "eval-run-1"},
		Results: []eval.CaseResult{
			{Name: "case-pass", Result: eval.TestResult{Passed: true, Expected: "ok", Actual: "ok", DurationMS: 11}},
			{Name: "case-regression", Result: eval.TestResult{Passed: false, Expected: "stable", Actual: "changed", DurationMS: 13, Error: "mismatch"}},
		},
		Metrics: eval.Metrics{
			CompletionRate:      0.5,
			ToolMisuseRate:      0.25,
			DelegationPrecision: 0.75,
			TokenCost:           28,
			TimeToCompletion:    24,
		},
	})
	if err != nil {
		t.Fatalf("save latest run: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/eval/results?limit=1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}

	var payload struct {
		Runs []struct {
			Suite    string           `json:"suite"`
			Identity eval.RunIdentity `json:"identity"`
			Status   string           `json:"status"`
			Total    int              `json:"total"`
			Passed   int              `json:"passed"`
			Failed   int              `json:"failed"`
			Metrics  eval.Metrics     `json:"metrics"`
			Baseline struct {
				Available   bool              `json:"available"`
				Regressions []eval.Regression `json:"regressions"`
			} `json:"baseline"`
		} `json:"runs"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 || len(payload.Runs) != 1 {
		t.Fatalf("expected one run in payload, got count=%d len=%d", payload.Count, len(payload.Runs))
	}

	run := payload.Runs[0]
	if run.Suite != "basic" {
		t.Fatalf("expected suite basic, got %q", run.Suite)
	}
	if run.Identity.InstanceID != "lab" || run.Identity.AgentID != "default" || run.Identity.RunID != "eval-run-2" || run.Identity.ParentRunID != "eval-run-1" {
		t.Fatalf("unexpected identity: %+v", run.Identity)
	}
	if run.Total != 2 || run.Passed != 1 || run.Failed != 1 {
		t.Fatalf("unexpected run summary: total=%d passed=%d failed=%d", run.Total, run.Passed, run.Failed)
	}
	if run.Status != "fail" {
		t.Fatalf("expected status fail, got %q", run.Status)
	}
	if run.Metrics.TokenCost != 28 || run.Metrics.TimeToCompletion != 24 {
		t.Fatalf("unexpected metrics: %+v", run.Metrics)
	}
	if !run.Baseline.Available {
		t.Fatalf("expected baseline available")
	}
	if len(run.Baseline.Regressions) != 1 || run.Baseline.Regressions[0].TestName != "case-regression" {
		t.Fatalf("expected one regression for case-regression, got %+v", run.Baseline.Regressions)
	}
}

func TestEvalResultsEndpointRejectsInvalidLimit(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/eval/results?limit=0", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}
}
