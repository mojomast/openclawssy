package dashboard

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var expectedDashboardPaths = []string{
	"/help",
	"/instances",
	"/workspace",
	"/secrets",
	"/docs",
	"/skills",
	"/chat",
	"/settings",
	"/runs",
	"/sessions",
	"/monitor",
	"/scheduler",
	"/sandbox",
	"/dashboards",
	"/wizard",
	"/agent-contract",
	"/prompt-stack",
	"/roles",
	"/delegation",
	"/eval",
}

func TestIntegrationDashboardNavigationAndRoutesCoverAllPages(t *testing.T) {
	srcRoot := dashboardUISrcDir(t)
	layoutContent := mustReadFile(t, filepath.Join(srcRoot, "components", "Layout.tsx"))
	appContent := mustReadFile(t, filepath.Join(srcRoot, "App.tsx"))

	navPathPattern := regexp.MustCompile(`\{\s*path:\s*'(/[^']+)'\s*,\s*label:`)
	navMatches := navPathPattern.FindAllStringSubmatch(layoutContent, -1)
	navPaths := make(map[string]struct{}, len(navMatches))
	for _, match := range navMatches {
		navPaths[strings.TrimSpace(match[1])] = struct{}{}
	}

	routePattern := regexp.MustCompile(`path="([^"]+)"`)
	routeMatches := routePattern.FindAllStringSubmatch(appContent, -1)
	routePaths := make(map[string]struct{}, len(routeMatches))
	for _, match := range routeMatches {
		route := strings.TrimSpace(match[1])
		if route == "" {
			continue
		}
		normalized := "/" + route
		if index := strings.Index(normalized, "/:"); index >= 0 {
			normalized = normalized[:index]
		}
		routePaths[normalized] = struct{}{}
	}

	if len(navPaths) != len(expectedDashboardPaths) {
		t.Fatalf("sidebar path count = %d, want %d", len(navPaths), len(expectedDashboardPaths))
	}

	for _, expected := range expectedDashboardPaths {
		if _, ok := navPaths[expected]; !ok {
			t.Fatalf("missing sidebar path %q; got %v", expected, sortedKeys(navPaths))
		}
		if _, ok := routePaths[expected]; !ok {
			t.Fatalf("missing app route for %q; got %v", expected, sortedKeys(routePaths))
		}
	}
}

func TestIntegrationDashboardPagesUseSharedShadcnUIComponents(t *testing.T) {
	pagesDir := filepath.Join(dashboardUISrcDir(t), "pages")
	pageFiles := []string{
		"AgentContractPage.tsx",
		"AgentMonitorPage.tsx",
		"ChatPage.tsx",
		"CustomDashboardsPage.tsx",
		"DelegationPage.tsx",
		"DocsPage.tsx",
		"EvalPage.tsx",
		"HelpPage.tsx",
		"PromptStackPage.tsx",
		"RoleTemplatePage.tsx",
		"RunsPage.tsx",
		"SandboxPage.tsx",
		"SchedulerPage.tsx",
		"SecretsPage.tsx",
		"SessionsPage.tsx",
		"SettingsPage.tsx",
		"SkillsPage.tsx",
		"WorkspacePage.tsx",
	}

	for _, name := range pageFiles {
		content := mustReadFile(t, filepath.Join(pagesDir, name))
		if !strings.Contains(content, "@/components/ui/") {
			t.Fatalf("page %s does not import shared shadcn/ui components", name)
		}
	}
}

func dashboardUISrcDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file path")
	}
	return filepath.Join(filepath.Dir(file), "ui", "src")
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
