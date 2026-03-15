package dashboard

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/eval"
)

type evalResultsResponse struct {
	Runs   []evalRunResponse `json:"runs"`
	Count  int               `json:"count"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
	Suite  string            `json:"suite,omitempty"`
}

type evalRunResponse struct {
	ID        int64                `json:"id"`
	Suite     string               `json:"suite"`
	Timestamp time.Time            `json:"timestamp"`
	Total     int                  `json:"total"`
	Passed    int                  `json:"passed"`
	Failed    int                  `json:"failed"`
	Status    string               `json:"status"`
	Results   []eval.CaseResult    `json:"results"`
	Metrics   eval.Metrics         `json:"metrics"`
	Baseline  evalBaselineResponse `json:"baseline"`
}

type evalBaselineResponse struct {
	Available   bool              `json:"available"`
	Timestamp   *time.Time        `json:"timestamp,omitempty"`
	Regressions []eval.Regression `json:"regressions"`
}

func (h *Handler) handleEvalResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query(), 25, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	suiteFilter := strings.TrimSpace(r.URL.Query().Get("suite"))

	store, err := eval.OpenStore(filepath.Join(h.rootDir, ".openclawssy", "eval", "results.db"))
	if err != nil {
		http.Error(w, "failed to open eval store", http.StatusInternalServerError)
		return
	}
	defer func() { _ = store.Close() }()

	rawRuns, err := store.ListRuns(r.Context(), suiteFilter, limit+offset)
	if err != nil {
		http.Error(w, "failed to load eval results", http.StatusInternalServerError)
		return
	}

	runs := rawRuns
	if offset > len(runs) {
		runs = []eval.SuiteRun{}
	} else {
		runs = runs[offset:]
	}
	if len(runs) > limit {
		runs = runs[:limit]
	}

	manager, err := eval.NewBaselineManager(store, h.rootDir)
	if err != nil {
		http.Error(w, "failed to initialize eval baseline manager", http.StatusInternalServerError)
		return
	}

	type baselineCache struct {
		run eval.SuiteRun
		ok  bool
	}
	baselinesBySuite := make(map[string]baselineCache)

	responseRuns := make([]evalRunResponse, 0, len(runs))
	for _, run := range runs {
		passed, failed := summarizeEvalRun(run.Results)

		baseline, cached := baselinesBySuite[run.Suite]
		if !cached {
			baselineRun, ok, loadErr := manager.LoadBaseline(run.Suite)
			if loadErr != nil {
				http.Error(w, "failed to load eval baseline", http.StatusInternalServerError)
				return
			}
			baseline = baselineCache{run: baselineRun, ok: ok}
			baselinesBySuite[run.Suite] = baseline
		}

		baselineResponse := evalBaselineResponse{
			Available:   baseline.ok,
			Regressions: []eval.Regression{},
		}
		if baseline.ok {
			baselineTimestamp := baseline.run.Timestamp
			baselineResponse.Timestamp = &baselineTimestamp
			baselineResponse.Regressions = eval.CompareRuns(baseline.run, run).Regressions
		}

		status := "pass"
		if failed > 0 {
			status = "fail"
		}

		responseRuns = append(responseRuns, evalRunResponse{
			ID:        run.ID,
			Suite:     run.Suite,
			Timestamp: run.Timestamp.UTC(),
			Total:     len(run.Results),
			Passed:    passed,
			Failed:    failed,
			Status:    status,
			Results:   run.Results,
			Metrics:   run.Metrics,
			Baseline:  baselineResponse,
		})
	}

	writeJSON(w, evalResultsResponse{
		Runs:   responseRuns,
		Count:  len(responseRuns),
		Limit:  limit,
		Offset: offset,
		Suite:  suiteFilter,
	})
}

func summarizeEvalRun(results []eval.CaseResult) (passed int, failed int) {
	for _, result := range results {
		if result.Result.Passed {
			passed++
		} else {
			failed++
		}
	}
	return passed, failed
}
