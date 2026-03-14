package eval

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

var (
	ErrInvalidSuiteName = errors.New("eval runner: suite name is required")
	ErrSuiteSetup       = errors.New("suite setup failed")
	ErrSuiteCleanup     = errors.New("suite cleanup failed")
	ErrNoRunFunction    = errors.New("suite run function is nil")
)

type Runner struct {
	store *Store
	nowFn func() time.Time
}

func NewRunner(store *Store) *Runner {
	return &Runner{
		store: store,
		nowFn: time.Now,
	}
}

func (r *Runner) RunSuite(ctx context.Context, suite Suite) (SuiteRun, error) {
	suiteName := strings.TrimSpace(suite.Name)
	if suiteName == "" {
		return SuiteRun{}, ErrInvalidSuiteName
	}

	report := SuiteRun{
		Suite:       suiteName,
		Description: suite.Description,
		Timestamp:   r.nowFn().UTC(),
		Results:     make([]CaseResult, 0, len(suite.TestCases)),
	}

	flatResults := make([]TestResult, 0, len(suite.TestCases))
	for _, testCase := range suite.TestCases {
		result := runCase(ctx, testCase)
		report.Results = append(report.Results, CaseResult{
			Name:        testCase.Name,
			Description: testCase.Description,
			Result:      result,
		})
		flatResults = append(flatResults, result)
	}

	report.Metrics = ComputeMetrics(flatResults)

	if r.store != nil {
		id, err := r.store.SaveRun(ctx, report)
		if err != nil {
			return SuiteRun{}, err
		}
		report.ID = id
	}

	return report, nil
}

func runCase(ctx context.Context, testCase TestCase) TestResult {
	start := time.Now()
	result := TestResult{}

	if testCase.Setup != nil {
		if err := runSetup(ctx, testCase.Setup); err != nil {
			result.Passed = false
			result.Error = fmt.Sprintf("%v: %v", ErrSuiteSetup, err)
		}
	}

	if result.Error == "" {
		if testCase.Run == nil {
			result.Passed = false
			result.Error = ErrNoRunFunction.Error()
		} else {
			runResult, err := runTestCase(ctx, testCase.Run)
			if err != nil {
				result.Passed = false
				result.Error = err.Error()
			} else {
				result = runResult
			}
		}
	}

	if testCase.Cleanup != nil {
		if err := runCleanup(ctx, testCase.Cleanup); err != nil {
			cleanupMsg := fmt.Sprintf("%v: %v", ErrSuiteCleanup, err)
			if strings.TrimSpace(result.Error) == "" {
				result.Error = cleanupMsg
			} else {
				result.Error = result.Error + "; " + cleanupMsg
			}
			result.Passed = false
		}
	}

	if result.DurationMS <= 0 {
		result.DurationMS = int(time.Since(start).Milliseconds())
		if result.DurationMS < 0 {
			result.DurationMS = 0
		}
	}

	if strings.TrimSpace(result.Error) != "" {
		result.Passed = false
	}

	return result
}

func runSetup(ctx context.Context, setup SetupFunc) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = callbackPanicError("setup", recovered)
		}
	}()

	return setup(ctx)
}

func runTestCase(ctx context.Context, run RunFunc) (result TestResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = callbackPanicError("run", recovered)
		}
	}()

	return run(ctx), nil
}

func runCleanup(ctx context.Context, cleanup CleanupFunc) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = callbackPanicError("cleanup", recovered)
		}
	}()

	return cleanup(ctx)
}

func callbackPanicError(stage string, recovered any) error {
	stack := strings.TrimSpace(string(debug.Stack()))
	if stack == "" {
		return fmt.Errorf("panic recovered in %s callback: %v", stage, recovered)
	}

	return fmt.Errorf("panic recovered in %s callback: %v\nstack:\n%s", stage, recovered, stack)
}
