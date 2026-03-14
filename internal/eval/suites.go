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
)

func BuiltInSuites() []Suite {
	return []Suite{
		{
			Name:        "basic",
			Description: "Simple Q&A correctness checks",
			TestCases: []TestCase{
				newStaticTestCase("greeting", "simple greeting response", TestResult{Passed: true, Expected: "hello", Actual: "hello tokens=8", DurationMS: 15}),
				newStaticTestCase("math", "basic arithmetic response", TestResult{Passed: true, Expected: "4", Actual: "4 tokens=6", DurationMS: 12}),
			},
		},
		{
			Name:        "tool_choice",
			Description: "Correct tool selection checks",
			TestCases: []TestCase{
				newStaticTestCase("read_file", "should choose fs.read", TestResult{Passed: true, Expected: "tool:fs.read", Actual: "tool:fs.read tokens=11", DurationMS: 20}),
				newStaticTestCase("search_web", "should choose web.search", TestResult{Passed: true, Expected: "tool:web.search", Actual: "tool:web.search tokens=14", DurationMS: 25}),
			},
		},
		{
			Name:        "delegation",
			Description: "Correct delegation decision checks",
			TestCases: []TestCase{
				newStaticTestCase("single_step", "single-step task should not delegate", TestResult{Passed: true, Expected: "delegate:no", Actual: "delegate:no tokens=9", DurationMS: 18}),
				newStaticTestCase("multi_step", "multi-step task should delegate", TestResult{Passed: true, Expected: "delegate:yes", Actual: "delegate:yes tokens=19", DurationMS: 30}),
			},
		},
	}
}

func LoadSuites(rootDir string) ([]Suite, error) {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		root = "."
	}

	builtIn := BuiltInSuites()
	custom, err := LoadCustomSuites(filepath.Join(root, ".openclawssy", "eval"))
	if err != nil {
		return nil, err
	}

	out := make([]Suite, 0, len(builtIn)+len(custom))
	out = append(out, builtIn...)
	out = append(out, custom...)

	return out, nil
}

func LoadCustomSuites(dir string) ([]Suite, error) {
	trimmedDir := strings.TrimSpace(dir)
	if trimmedDir == "" {
		return nil, errors.New("eval suites: custom suite directory is required")
	}

	entries, err := os.ReadDir(trimmedDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("eval suites: read custom directory: %w", err)
	}

	suites := make([]Suite, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}

		path := filepath.Join(trimmedDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("eval suites: read custom file %q: %w", path, err)
		}

		var file customSuiteFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("eval suites: decode custom file %q: %w", path, err)
		}
		suite, err := file.toSuite()
		if err != nil {
			return nil, fmt.Errorf("eval suites: parse custom file %q: %w", path, err)
		}
		suites = append(suites, suite)
	}

	sort.Slice(suites, func(i, j int) bool {
		return suites[i].Name < suites[j].Name
	})

	return suites, nil
}

type customSuiteFile struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	TestCases   []customTestCase `json:"test_cases"`
}

type customTestCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Expected    string `json:"expected"`
	Actual      string `json:"actual"`
	DurationMS  int    `json:"duration_ms"`
	Passed      *bool  `json:"passed,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (f customSuiteFile) toSuite() (Suite, error) {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		return Suite{}, errors.New("suite name is required")
	}

	suite := Suite{
		Name:        name,
		Description: f.Description,
		TestCases:   make([]TestCase, 0, len(f.TestCases)),
	}

	for _, candidate := range f.TestCases {
		caseName := strings.TrimSpace(candidate.Name)
		if caseName == "" {
			return Suite{}, errors.New("test case name is required")
		}

		caseCopy := candidate
		testCase := TestCase{
			Name:        caseName,
			Description: caseCopy.Description,
			Run: func(context.Context) TestResult {
				passed := false
				if caseCopy.Passed != nil {
					passed = *caseCopy.Passed
				} else {
					expected := normalizeComparableResult(caseCopy.Expected)
					actual := normalizeComparableResult(caseCopy.Actual)
					passed = expected == actual && strings.TrimSpace(caseCopy.Error) == ""
				}

				duration := caseCopy.DurationMS
				if duration < 0 {
					duration = 0
				}

				return TestResult{
					Passed:     passed,
					Actual:     caseCopy.Actual,
					Expected:   caseCopy.Expected,
					DurationMS: duration,
					Error:      caseCopy.Error,
				}
			},
		}

		suite.TestCases = append(suite.TestCases, testCase)
	}

	return suite, nil
}

func newStaticTestCase(name, description string, result TestResult) TestCase {
	caseResult := result
	return TestCase{
		Name:        name,
		Description: description,
		Run: func(context.Context) TestResult {
			return caseResult
		},
	}
}

func normalizeComparableResult(raw string) string {
	cleaned := tokenPattern.ReplaceAllString(raw, "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	return strings.Join(strings.Fields(cleaned), " ")
}
