package eval

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunnerExecutesSuiteAndPersistsResults(t *testing.T) {
	t.Parallel()

	store, err := OpenStore(filepath.Join(t.TempDir(), "eval.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	order := make([]string, 0, 3)
	suite := Suite{
		Name:        "runner-suite",
		Description: "suite used for runner persistence validation",
		TestCases: []TestCase{
			{
				Name:        "case-1",
				Description: "records setup/run/cleanup order",
				Setup: func(context.Context) error {
					order = append(order, "setup")
					return nil
				},
				Run: func(context.Context) TestResult {
					order = append(order, "run")
					return TestResult{Passed: true, Expected: "tool:fs.read", Actual: "tool:fs.read tokens=11", DurationMS: 12}
				},
				Cleanup: func(context.Context) error {
					order = append(order, "cleanup")
					return nil
				},
			},
		},
	}

	runner := NewRunner(store)
	report, err := runner.RunSuite(context.Background(), suite)
	if err != nil {
		t.Fatalf("RunSuite() error = %v", err)
	}

	if !reflect.DeepEqual(order, []string{"setup", "run", "cleanup"}) {
		t.Fatalf("execution order = %#v, want %#v", order, []string{"setup", "run", "cleanup"})
	}
	if report.Suite != "runner-suite" {
		t.Fatalf("report suite = %q, want %q", report.Suite, "runner-suite")
	}
	if len(report.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(report.Results))
	}
	if report.Metrics.TokenCost != 11 {
		t.Fatalf("report token cost = %d, want 11", report.Metrics.TokenCost)
	}

	latest, ok, err := store.LatestRun(context.Background(), "runner-suite")
	if err != nil {
		t.Fatalf("LatestRun() error = %v", err)
	}
	if !ok {
		t.Fatal("LatestRun() found = false, want true")
	}
	if len(latest.Results) != 1 {
		t.Fatalf("persisted result count = %d, want 1", len(latest.Results))
	}
	if latest.Results[0].Name != "case-1" {
		t.Fatalf("persisted case name = %q, want %q", latest.Results[0].Name, "case-1")
	}
}

func TestRunnerHandlesSetupAndCleanupErrors(t *testing.T) {
	t.Parallel()

	runCalled := false
	runner := NewRunner(nil)
	report, err := runner.RunSuite(context.Background(), Suite{
		Name: "errors-suite",
		TestCases: []TestCase{
			{
				Name: "case-err",
				Setup: func(context.Context) error {
					return ErrSuiteSetup
				},
				Run: func(context.Context) TestResult {
					runCalled = true
					return TestResult{Passed: true}
				},
				Cleanup: func(context.Context) error {
					return ErrSuiteCleanup
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RunSuite() error = %v", err)
	}
	if runCalled {
		t.Fatal("run function should not execute when setup fails")
	}
	if len(report.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(report.Results))
	}
	if report.Results[0].Result.Passed {
		t.Fatal("result should fail when setup fails")
	}
	errText := report.Results[0].Result.Error
	if !strings.Contains(errText, ErrSuiteSetup.Error()) {
		t.Fatalf("result error %q missing setup marker %q", errText, ErrSuiteSetup.Error())
	}
	if !strings.Contains(errText, ErrSuiteCleanup.Error()) {
		t.Fatalf("result error %q missing cleanup marker %q", errText, ErrSuiteCleanup.Error())
	}
}
