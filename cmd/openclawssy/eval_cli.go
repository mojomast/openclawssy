package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"openclawssy/internal/eval"
	"openclawssy/internal/runtime"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

type evalService struct {
	engine *runtime.Engine
	out    io.Writer
	err    io.Writer
}

func (s evalService) HandleEval(ctx context.Context, args []string) int {
	if len(args) == 0 {
		printEvalUsage(s.stdout())
		return 0
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	subArgs := args[1:]

	switch command {
	case "-h", "--help", "help":
		printEvalUsage(s.stdout())
		return 0
	case "run":
		return s.handleEvalRun(ctx, subArgs)
	case "list":
		return s.handleEvalList(subArgs)
	case "results":
		return s.handleEvalResults(ctx, subArgs)
	case "baseline":
		return s.handleEvalBaseline(ctx, subArgs)
	case "compare":
		return s.handleEvalCompare(ctx, subArgs)
	default:
		fmt.Fprintf(s.stderr(), "unknown eval subcommand: %s\n\n", args[0])
		printEvalUsage(s.stderr())
		return 2
	}
}

func (s evalService) handleEvalRun(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("eval run", flag.ContinueOnError)
	fs.SetOutput(s.stderr())
	var suiteName string
	var identity eval.RunIdentity
	fs.StringVar(&suiteName, "suite", "", "suite name or 'all'")
	fs.StringVar(&identity.InstanceID, "instance-id", "", "optional instance identity metadata")
	fs.StringVar(&identity.AgentID, "agent-id", "", "optional agent identity metadata")
	fs.StringVar(&identity.RunID, "run-id", "", "optional run identity metadata")
	fs.StringVar(&identity.ParentRunID, "parent-run-id", "", "optional parent run identity metadata")
	fs.StringVar(&identity.RootRunID, "root-run-id", "", "optional root run identity metadata")
	fs.StringVar(&identity.Source, "source", "", "optional source metadata")
	fs.StringVar(&identity.TaskID, "task-id", "", "optional task metadata")
	fs.StringVar(&identity.SessionID, "session-id", "", "optional session metadata")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(suiteName) == "" {
		fmt.Fprintln(s.stderr(), "eval run requires --suite <name|all>")
		return 2
	}

	suites, err := eval.LoadSuites(s.rootDir())
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	if len(suites) == 0 {
		fmt.Fprintln(s.stderr(), "no eval suites are available")
		return 1
	}

	targetSuites, err := selectSuites(suites, suiteName)
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}

	store, err := eval.OpenStore(filepath.Join(s.rootDir(), ".openclawssy", "eval", "results.db"))
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	defer store.Close()

	runner := eval.NewRunner(store)

	totalCases := 0
	totalPassed := 0
	totalFailed := 0
	for idx, suite := range targetSuites {
		fmt.Fprintf(s.stdout(), "%sRunning suite %d/%d: %s%s\n", ansiCyan, idx+1, len(targetSuites), suite.Name, ansiReset)

		report, runErr := runner.RunSuiteWithOptions(ctx, suite, eval.RunOptions{Identity: identity})
		if runErr != nil {
			fmt.Fprintln(s.stderr(), runErr)
			return 1
		}
		printSuiteRunReport(s.stdout(), report)

		passed, failed := summarizeCaseResults(report.Results)
		totalCases += len(report.Results)
		totalPassed += passed
		totalFailed += failed
	}

	status := colorize(ansiGreen, "PASS")
	if totalFailed > 0 {
		status = colorize(ansiRed, "FAIL")
	}
	fmt.Fprintf(s.stdout(), "%sOverall:%s total=%d passed=%d failed=%d status=%s\n", ansiBold, ansiReset, totalCases, totalPassed, totalFailed, status)

	if totalFailed > 0 {
		return 1
	}
	return 0
}

func (s evalService) handleEvalList(args []string) int {
	fs := flag.NewFlagSet("eval list", flag.ContinueOnError)
	fs.SetOutput(s.stderr())
	if err := fs.Parse(args); err != nil {
		return 2
	}

	suites, err := eval.LoadSuites(s.rootDir())
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}

	sorted := append([]eval.Suite(nil), suites...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	fmt.Fprintln(s.stdout(), "Available eval suites:")
	for _, suite := range sorted {
		fmt.Fprintf(s.stdout(), "- %s | %s | cases=%d\n", suite.Name, strings.TrimSpace(suite.Description), len(suite.TestCases))
	}

	return 0
}

func (s evalService) handleEvalResults(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("eval results", flag.ContinueOnError)
	fs.SetOutput(s.stderr())
	var suiteName string
	var limit int
	fs.StringVar(&suiteName, "suite", "", "optional suite filter")
	fs.IntVar(&limit, "limit", 20, "maximum number of runs")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, err := eval.OpenStore(filepath.Join(s.rootDir(), ".openclawssy", "eval", "results.db"))
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	defer store.Close()

	runs, err := store.ListRuns(ctx, strings.TrimSpace(suiteName), limit)
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	if len(runs) == 0 {
		fmt.Fprintf(s.stdout(), "%sNo eval results found.%s\n", ansiYellow, ansiReset)
		return 0
	}

	fmt.Fprintln(s.stdout(), "Eval run history:")
	for _, run := range runs {
		passed, failed := summarizeCaseResults(run.Results)
		status := colorize(ansiGreen, "PASS")
		if failed > 0 {
			status = colorize(ansiRed, "FAIL")
		}
		fmt.Fprintf(
			s.stdout(),
			"- %s suite=%s status=%s total=%d passed=%d failed=%d completion=%.1f%%\n",
			run.Timestamp.UTC().Format("2006-01-02 15:04:05Z07:00"),
			run.Suite,
			status,
			len(run.Results),
			passed,
			failed,
			run.Metrics.CompletionRate*100,
		)
	}

	return 0
}

func (s evalService) handleEvalBaseline(ctx context.Context, args []string) int {
	if len(args) == 0 {
		printEvalBaselineUsage(s.stderr())
		return 2
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	subArgs := args[1:]

	switch command {
	case "-h", "--help", "help":
		printEvalBaselineUsage(s.stdout())
		return 0
	case "set":
		return s.handleEvalBaselineSet(ctx, subArgs)
	default:
		fmt.Fprintf(s.stderr(), "unknown eval baseline subcommand: %s\n", args[0])
		printEvalBaselineUsage(s.stderr())
		return 2
	}
}

func (s evalService) handleEvalBaselineSet(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("eval baseline set", flag.ContinueOnError)
	fs.SetOutput(s.stderr())
	var suiteName string
	fs.StringVar(&suiteName, "suite", "all", "suite name or 'all'")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, err := eval.OpenStore(filepath.Join(s.rootDir(), ".openclawssy", "eval", "results.db"))
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	defer store.Close()

	manager, err := eval.NewBaselineManager(store, s.rootDir())
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}

	target := strings.TrimSpace(suiteName)
	if target == "" {
		target = "all"
	}

	if strings.EqualFold(target, "all") {
		suiteNames, runErr := listSuiteNamesFromRuns(ctx, store)
		if runErr != nil {
			fmt.Fprintln(s.stderr(), runErr)
			return 1
		}
		if len(suiteNames) == 0 {
			fmt.Fprintln(s.stderr(), eval.ErrNoSuiteRuns)
			return 1
		}

		for _, candidate := range suiteNames {
			if saveErr := manager.SaveBaseline(ctx, candidate); saveErr != nil {
				fmt.Fprintf(s.stderr(), "save baseline for %q: %v\n", candidate, saveErr)
				return 1
			}
			fmt.Fprintf(s.stdout(), "%sSaved baseline%s for suite %s\n", ansiGreen, ansiReset, candidate)
		}
		return 0
	}

	if err := manager.SaveBaseline(ctx, target); err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	fmt.Fprintf(s.stdout(), "%sSaved baseline%s for suite %s\n", ansiGreen, ansiReset, target)
	return 0
}

func (s evalService) handleEvalCompare(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("eval compare", flag.ContinueOnError)
	fs.SetOutput(s.stderr())
	var suiteName string
	fs.StringVar(&suiteName, "suite", "all", "suite name or 'all'")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, err := eval.OpenStore(filepath.Join(s.rootDir(), ".openclawssy", "eval", "results.db"))
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}
	defer store.Close()

	manager, err := eval.NewBaselineManager(store, s.rootDir())
	if err != nil {
		fmt.Fprintln(s.stderr(), err)
		return 1
	}

	target := strings.TrimSpace(suiteName)
	if target == "" {
		target = "all"
	}

	suitesToCompare := make([]string, 0)
	if strings.EqualFold(target, "all") {
		names, namesErr := listSuiteNamesForCompare(ctx, store, s.rootDir())
		if namesErr != nil {
			fmt.Fprintln(s.stderr(), namesErr)
			return 1
		}
		suitesToCompare = append(suitesToCompare, names...)
	} else {
		suitesToCompare = append(suitesToCompare, target)
	}

	if len(suitesToCompare) == 0 {
		fmt.Fprintln(s.stderr(), "no suites available to compare")
		return 1
	}

	comparedSuites := 0
	regressionCount := 0
	for _, candidate := range suitesToCompare {
		comparison, compareErr := manager.CompareLatest(ctx, candidate)
		if compareErr != nil {
			switch {
			case errors.Is(compareErr, eval.ErrBaselineNotFound):
				fmt.Fprintf(s.stdout(), "%sSkipping suite %s: no baseline saved%s\n", ansiYellow, candidate, ansiReset)
				continue
			case errors.Is(compareErr, eval.ErrNoSuiteRuns):
				fmt.Fprintf(s.stdout(), "%sSkipping suite %s: no latest run found%s\n", ansiYellow, candidate, ansiReset)
				continue
			default:
				fmt.Fprintf(s.stderr(), "compare suite %q: %v\n", candidate, compareErr)
				return 1
			}
		}

		comparedSuites++
		regressionCount += len(comparison.Regressions)
		printComparisonReport(s.stdout(), comparison)
	}

	if comparedSuites == 0 {
		fmt.Fprintln(s.stderr(), "no suites had both baseline and latest run data")
		return 1
	}

	if regressionCount > 0 {
		fmt.Fprintf(s.stdout(), "%sComparison complete:%s regressions=%d\n", ansiRed, ansiReset, regressionCount)
		return 1
	}

	fmt.Fprintf(s.stdout(), "%sComparison complete:%s regressions=0\n", ansiGreen, ansiReset)
	return 0
}

func (s evalService) rootDir() string {
	return "."
}

func (s evalService) stdout() io.Writer {
	if s.out != nil {
		return s.out
	}
	return os.Stdout
}

func (s evalService) stderr() io.Writer {
	if s.err != nil {
		return s.err
	}
	return os.Stderr
}

func summarizeCaseResults(results []eval.CaseResult) (passed int, failed int) {
	for _, result := range results {
		if result.Result.Passed {
			passed++
		} else {
			failed++
		}
	}
	return passed, failed
}

func printSuiteRunReport(w io.Writer, run eval.SuiteRun) {
	fmt.Fprintf(w, "%sSuite: %s%s\n", ansiBold, run.Suite, ansiReset)
	for _, caseResult := range run.Results {
		status := colorize(ansiGreen, "PASS")
		if !caseResult.Result.Passed {
			status = colorize(ansiRed, "FAIL")
		}
		fmt.Fprintf(w, "  %s %s (%dms)\n", status, caseResult.Name, caseResult.Result.DurationMS)
		if !caseResult.Result.Passed {
			if strings.TrimSpace(caseResult.Result.Expected) != "" {
				fmt.Fprintf(w, "    expected: %s\n", strings.TrimSpace(caseResult.Result.Expected))
			}
			if strings.TrimSpace(caseResult.Result.Actual) != "" {
				fmt.Fprintf(w, "    actual:   %s\n", strings.TrimSpace(caseResult.Result.Actual))
			}
			if strings.TrimSpace(caseResult.Result.Error) != "" {
				fmt.Fprintf(w, "    error:    %s\n", strings.TrimSpace(caseResult.Result.Error))
			}
		}
	}

	passed, failed := summarizeCaseResults(run.Results)
	fmt.Fprintf(
		w,
		"  summary: total=%d passed=%d failed=%d completion_rate=%.1f%%\n",
		len(run.Results),
		passed,
		failed,
		run.Metrics.CompletionRate*100,
	)
	fmt.Fprintf(
		w,
		"  metrics: completion_rate=%.3f tool_misuse_rate=%.3f delegation_precision=%.3f unnecessary_delegation_rate=%.3f token_cost=%d time_to_completion=%dms\n",
		run.Metrics.CompletionRate,
		run.Metrics.ToolMisuseRate,
		run.Metrics.DelegationPrecision,
		run.Metrics.UnnecessaryDelegationRate,
		run.Metrics.TokenCost,
		run.Metrics.TimeToCompletion,
	)
	if strings.TrimSpace(run.Identity.InstanceID) != "" || strings.TrimSpace(run.Identity.AgentID) != "" || strings.TrimSpace(run.Identity.RunID) != "" {
		fmt.Fprintf(w, "  identity: instance=%s agent=%s run=%s parent=%s root=%s source=%s task=%s session=%s\n",
			blankDash(run.Identity.InstanceID),
			blankDash(run.Identity.AgentID),
			blankDash(run.Identity.RunID),
			blankDash(run.Identity.ParentRunID),
			blankDash(run.Identity.RootRunID),
			blankDash(run.Identity.Source),
			blankDash(run.Identity.TaskID),
			blankDash(run.Identity.SessionID),
		)
	}
}

func blankDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func printComparisonReport(w io.Writer, comparison eval.BaselineComparison) {
	basePassed, baseFailed := summarizeCaseResults(comparison.Baseline.Results)
	latestPassed, latestFailed := summarizeCaseResults(comparison.Latest.Results)

	fmt.Fprintf(
		w,
		"%sSuite %s%s baseline=%d/%d latest=%d/%d regressions=%d\n",
		ansiBold,
		comparison.Suite,
		ansiReset,
		basePassed,
		baseFailed,
		latestPassed,
		latestFailed,
		len(comparison.Regressions),
	)

	if len(comparison.Regressions) == 0 {
		fmt.Fprintf(w, "  %sNo regressions detected%s\n", ansiGreen, ansiReset)
		return
	}

	for _, regression := range comparison.Regressions {
		fmt.Fprintf(
			w,
			"  %sREGRESSION%s %s (baseline=pass latest=fail)\n",
			ansiRed,
			ansiReset,
			regression.TestName,
		)
	}
}

func selectSuites(suites []eval.Suite, requested string) ([]eval.Suite, error) {
	normalized := strings.ToLower(strings.TrimSpace(requested))
	if normalized == "" {
		return nil, errors.New("suite name is required")
	}
	if normalized == "all" {
		selected := append([]eval.Suite(nil), suites...)
		sort.Slice(selected, func(i, j int) bool {
			return selected[i].Name < selected[j].Name
		})
		return selected, nil
	}

	for _, suite := range suites {
		if strings.EqualFold(strings.TrimSpace(suite.Name), normalized) {
			return []eval.Suite{suite}, nil
		}
	}

	return nil, fmt.Errorf("eval suite %q not found", requested)
}

func listSuiteNamesFromRuns(ctx context.Context, store *eval.Store) ([]string, error) {
	runs, err := store.ListRuns(ctx, "", 20000)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{})
	for _, run := range runs {
		name := strings.TrimSpace(run.Suite)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}

	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)

	return names, nil
}

func listSuiteNamesForCompare(ctx context.Context, store *eval.Store, root string) ([]string, error) {
	runSuites, err := listSuiteNamesFromRuns(ctx, store)
	if err != nil {
		return nil, err
	}
	baselineSuites, err := listSuiteNamesFromBaselines(root)
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(runSuites)+len(baselineSuites))
	for _, name := range runSuites {
		set[name] = struct{}{}
	}
	for _, name := range baselineSuites {
		set[name] = struct{}{}
	}

	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func listSuiteNamesFromBaselines(root string) ([]string, error) {
	baselineDir := filepath.Join(root, ".openclawssy", "eval", "baselines")
	entries, err := os.ReadDir(baselineDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func colorize(color, text string) string {
	return color + text + ansiReset
}

func printEvalUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: openclawssy eval <command> [flags]")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  run --suite <name|all>  Run one suite or all suites and persist results")
	fmt.Fprintln(w, "  list                    List available suites with description and case count")
	fmt.Fprintln(w, "  results                 Show persisted run history")
	fmt.Fprintln(w, "  baseline set            Save latest run(s) as baseline")
	fmt.Fprintln(w, "  compare                 Compare latest run(s) against baseline and flag regressions")
	fmt.Fprintln(w, "examples:")
	fmt.Fprintln(w, "  openclawssy eval run --suite basic")
	fmt.Fprintln(w, "  openclawssy eval run --suite all")
	fmt.Fprintln(w, "  openclawssy eval list")
	fmt.Fprintln(w, "  openclawssy eval results")
	fmt.Fprintln(w, "  openclawssy eval baseline set")
	fmt.Fprintln(w, "  openclawssy eval compare")
}

func printEvalBaselineUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: openclawssy eval baseline set [--suite <name|all>]")
}
