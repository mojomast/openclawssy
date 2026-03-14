package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltInSuitesContainExpectedSuites(t *testing.T) {
	t.Parallel()

	suites := BuiltInSuites()
	if len(suites) < 3 {
		t.Fatalf("built-in suites count = %d, want at least 3", len(suites))
	}

	byName := map[string]Suite{}
	for _, suite := range suites {
		byName[suite.Name] = suite
	}

	for _, name := range []string{"basic", "tool_choice", "delegation"} {
		suite, ok := byName[name]
		if !ok {
			t.Fatalf("missing built-in suite %q", name)
		}
		if len(suite.TestCases) == 0 {
			t.Fatalf("built-in suite %q has no test cases", name)
		}
		for _, tc := range suite.TestCases {
			if tc.Run == nil {
				t.Fatalf("suite %q case %q has nil Run function", name, tc.Name)
			}
		}
	}
}

func TestLoadCustomSuitesFromJSON(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), ".openclawssy", "eval")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	payload := map[string]any{
		"name":        "custom_suite",
		"description": "custom suite from json",
		"test_cases": []map[string]any{
			{
				"name":        "custom_case",
				"description": "custom case",
				"expected":    "delegate:no",
				"actual":      "delegate:no tokens=7",
				"duration_ms": 42,
			},
		},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom_suite.json"), raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	suites, err := LoadCustomSuites(dir)
	if err != nil {
		t.Fatalf("LoadCustomSuites() error = %v", err)
	}
	if len(suites) != 1 {
		t.Fatalf("custom suites count = %d, want 1", len(suites))
	}

	suite := suites[0]
	if suite.Name != "custom_suite" {
		t.Fatalf("suite name = %q, want %q", suite.Name, "custom_suite")
	}
	if len(suite.TestCases) != 1 {
		t.Fatalf("test case count = %d, want 1", len(suite.TestCases))
	}

	result := suite.TestCases[0].Run(context.Background())
	if !result.Passed {
		t.Fatalf("custom case should pass, got result=%+v", result)
	}
	if result.DurationMS != 42 {
		t.Fatalf("custom case duration = %d, want 42", result.DurationMS)
	}
}

func TestLoadSuitesIncludesBuiltInAndCustom(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".openclawssy", "eval")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	raw := []byte(`{"name":"extra","description":"extra suite","test_cases":[{"name":"case","expected":"ok","actual":"ok","duration_ms":1}]}`)
	if err := os.WriteFile(filepath.Join(dir, "extra.json"), raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	suites, err := LoadSuites(root)
	if err != nil {
		t.Fatalf("LoadSuites() error = %v", err)
	}

	foundExtra := false
	foundBuiltIn := false
	for _, suite := range suites {
		if suite.Name == "extra" {
			foundExtra = true
		}
		if suite.Name == "basic" {
			foundBuiltIn = true
		}
	}

	if !foundExtra {
		t.Fatal("expected custom suite " + `"extra"` + " in combined suite list")
	}
	if !foundBuiltIn {
		t.Fatal("expected built-in suite " + `"basic"` + " in combined suite list")
	}
}
