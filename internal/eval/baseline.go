package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"openclawssy/internal/fsutil"
)

var (
	ErrBaselineNotFound = errors.New("eval baseline: baseline not found")
	ErrNoSuiteRuns      = errors.New("eval baseline: no suite runs found")
)

type Regression struct {
	TestName string     `json:"test_name"`
	Baseline TestResult `json:"baseline"`
	Latest   TestResult `json:"latest"`
}

type BaselineComparison struct {
	Suite       string       `json:"suite"`
	Baseline    SuiteRun     `json:"baseline"`
	Latest      SuiteRun     `json:"latest"`
	Regressions []Regression `json:"regressions"`
}

type BaselineManager struct {
	store       *Store
	baselineDir string
}

func NewBaselineManager(store *Store, rootDir string) (*BaselineManager, error) {
	if store == nil {
		return nil, errors.New("eval baseline: store is required")
	}
	root := strings.TrimSpace(rootDir)
	if root == "" {
		root = "."
	}
	return &BaselineManager{
		store:       store,
		baselineDir: filepath.Join(root, ".openclawssy", "eval", "baselines"),
	}, nil
}

func (m *BaselineManager) SaveBaseline(ctx context.Context, suite string) error {
	latest, ok, err := m.store.LatestRun(ctx, suite)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoSuiteRuns
	}

	path, err := m.baselinePath(suite)
	if err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(latest, "", "  ")
	if err != nil {
		return fmt.Errorf("eval baseline: encode baseline: %w", err)
	}
	encoded = append(encoded, '\n')

	if err := fsutil.WriteFileAtomic(path, encoded, 0o600); err != nil {
		return fmt.Errorf("eval baseline: write baseline: %w", err)
	}
	return nil
}

func (m *BaselineManager) CompareLatest(ctx context.Context, suite string) (BaselineComparison, error) {
	baseline, ok, err := m.LoadBaseline(suite)
	if err != nil {
		return BaselineComparison{}, err
	}
	if !ok {
		return BaselineComparison{}, ErrBaselineNotFound
	}

	latest, ok, err := m.store.LatestRun(ctx, suite)
	if err != nil {
		return BaselineComparison{}, err
	}
	if !ok {
		return BaselineComparison{}, ErrNoSuiteRuns
	}

	return CompareRuns(baseline, latest), nil
}

func (m *BaselineManager) LoadBaseline(suite string) (SuiteRun, bool, error) {
	path, err := m.baselinePath(suite)
	if err != nil {
		return SuiteRun{}, false, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SuiteRun{}, false, nil
		}
		return SuiteRun{}, false, fmt.Errorf("eval baseline: read baseline: %w", err)
	}

	var run SuiteRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return SuiteRun{}, false, fmt.Errorf("eval baseline: decode baseline: %w", err)
	}
	return run, true, nil
}

func CompareRuns(baseline, latest SuiteRun) BaselineComparison {
	comparison := BaselineComparison{
		Suite:       latest.Suite,
		Baseline:    baseline,
		Latest:      latest,
		Regressions: DetectRegressions(baseline.Results, latest.Results),
	}
	if strings.TrimSpace(comparison.Suite) == "" {
		comparison.Suite = baseline.Suite
	}
	return comparison
}

func DetectRegressions(baselineResults, latestResults []CaseResult) []Regression {
	baselineByName := make(map[string]TestResult, len(baselineResults))
	for _, result := range baselineResults {
		baselineByName[result.Name] = result.Result
	}

	regressions := make([]Regression, 0)
	for _, latest := range latestResults {
		baseline, ok := baselineByName[latest.Name]
		if !ok {
			continue
		}
		if baseline.Passed && !latest.Result.Passed {
			regressions = append(regressions, Regression{
				TestName: latest.Name,
				Baseline: baseline,
				Latest:   latest.Result,
			})
		}
	}

	sort.Slice(regressions, func(i, j int) bool {
		return regressions[i].TestName < regressions[j].TestName
	})

	return regressions
}

func (m *BaselineManager) baselinePath(suite string) (string, error) {
	normalizedSuite := strings.TrimSpace(suite)
	if normalizedSuite == "" {
		return "", errors.New("eval baseline: suite is required")
	}
	if strings.Contains(normalizedSuite, "..") || strings.ContainsAny(normalizedSuite, `/\\`) {
		return "", errors.New("eval baseline: invalid suite")
	}
	return filepath.Join(m.baselineDir, normalizedSuite+".json"), nil
}
