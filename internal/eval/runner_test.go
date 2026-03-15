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

func TestRunnerRecoversFromCallbackPanics(t *testing.T) {
	t.Parallel()

	setupRunCalled := false
	setupCleanupCalled := false

	runner := NewRunner(nil)
	report, err := runner.RunSuite(context.Background(), Suite{
		Name: "panic-recovery-suite",
		TestCases: []TestCase{
			{
				Name: "setup-panic",
				Setup: func(context.Context) error {
					panic("setup boom")
				},
				Run: func(context.Context) TestResult {
					setupRunCalled = true
					return TestResult{Passed: true}
				},
				Cleanup: func(context.Context) error {
					setupCleanupCalled = true
					return nil
				},
			},
			{
				Name: "run-panic",
				Run: func(context.Context) TestResult {
					panic("run boom")
				},
			},
			{
				Name: "cleanup-panic",
				Run: func(context.Context) TestResult {
					return TestResult{Passed: true}
				},
				Cleanup: func(context.Context) error {
					panic("cleanup boom")
				},
			},
			{
				Name: "post-panic-case",
				Run: func(context.Context) TestResult {
					return TestResult{Passed: true}
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RunSuite() error = %v", err)
	}

	if len(report.Results) != 4 {
		t.Fatalf("result count = %d, want 4", len(report.Results))
	}

	if setupRunCalled {
		t.Fatal("setup panic case run callback should not execute")
	}
	if !setupCleanupCalled {
		t.Fatal("setup panic case cleanup callback should still execute")
	}

	setupErr := report.Results[0].Result.Error
	if report.Results[0].Result.Passed {
		t.Fatal("setup panic case should fail")
	}
	if !strings.Contains(setupErr, ErrSuiteSetup.Error()) {
		t.Fatalf("setup panic error %q missing setup marker %q", setupErr, ErrSuiteSetup.Error())
	}
	if !strings.Contains(setupErr, "panic recovered in setup callback") {
		t.Fatalf("setup panic error %q missing panic recovery context", setupErr)
	}
	if !strings.Contains(setupErr, "setup boom") {
		t.Fatalf("setup panic error %q missing panic value", setupErr)
	}

	runErr := report.Results[1].Result.Error
	if report.Results[1].Result.Passed {
		t.Fatal("run panic case should fail")
	}
	if !strings.Contains(runErr, "panic recovered in run callback") {
		t.Fatalf("run panic error %q missing panic recovery context", runErr)
	}
	if !strings.Contains(runErr, "run boom") {
		t.Fatalf("run panic error %q missing panic value", runErr)
	}

	cleanupErr := report.Results[2].Result.Error
	if report.Results[2].Result.Passed {
		t.Fatal("cleanup panic case should fail")
	}
	if !strings.Contains(cleanupErr, ErrSuiteCleanup.Error()) {
		t.Fatalf("cleanup panic error %q missing cleanup marker %q", cleanupErr, ErrSuiteCleanup.Error())
	}
	if !strings.Contains(cleanupErr, "panic recovered in cleanup callback") {
		t.Fatalf("cleanup panic error %q missing panic recovery context", cleanupErr)
	}
	if !strings.Contains(cleanupErr, "cleanup boom") {
		t.Fatalf("cleanup panic error %q missing panic value", cleanupErr)
	}

	if !report.Results[3].Result.Passed {
		t.Fatalf("post panic case should pass, got result %#v", report.Results[3].Result)
	}
}
