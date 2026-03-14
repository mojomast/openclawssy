package roles

import (
	"strings"
	"testing"
)

func TestRouteSelectsExpectedBuiltInRoles(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil)

	tests := []struct {
		name     string
		task     string
		wantRole string
	}{
		{name: "scout", task: "read and search the repository for TODO references", wantRole: "scout"},
		{name: "planner", task: "plan and decompose this migration into phases", wantRole: "planner"},
		{name: "implementer", task: "write and implement a patch for the failing API handler", wantRole: "implementer"},
		{name: "verifier", task: "test and verify the new endpoint behavior", wantRole: "verifier"},
		{name: "reviewer", task: "review and audit this change for security risks", wantRole: "reviewer"},
		{name: "operator", task: "configure setup settings for this runtime", wantRole: "operator"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			selection := router.Route(tt.task)
			if selection.Template.Name != tt.wantRole {
				t.Fatalf("Route(%q) role = %q, want %q", tt.task, selection.Template.Name, tt.wantRole)
			}
			if strings.TrimSpace(selection.Rationale) == "" {
				t.Fatalf("Route(%q) rationale is empty", tt.task)
			}
			if selection.Confidence <= 0 {
				t.Fatalf("Route(%q) confidence = %f, want > 0", tt.task, selection.Confidence)
			}
		})
	}
}

func TestRouteFallbackToImplementerWhenNoClearMatch(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil)
	selection := router.Route("please help with this sometime")

	if selection.Template.Name != "implementer" {
		t.Fatalf("fallback role = %q, want implementer", selection.Template.Name)
	}
	if !strings.Contains(strings.ToLower(selection.Rationale), "fallback") {
		t.Fatalf("fallback rationale = %q, want mention of fallback", selection.Rationale)
	}
}

func TestRouteConfidenceReflectsKeywordDensity(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil)

	high := router.Route("write implement fix patch this file and implement another fix")
	low := router.Route("implement this")

	if high.Template.Name != "implementer" {
		t.Fatalf("high-density role = %q, want implementer", high.Template.Name)
	}
	if low.Template.Name != "implementer" {
		t.Fatalf("low-density role = %q, want implementer", low.Template.Name)
	}
	if high.Confidence <= low.Confidence {
		t.Fatalf("expected high-density confidence (%f) > low-density confidence (%f)", high.Confidence, low.Confidence)
	}
}

func TestRouteRationaleReferencesMatchedKeywords(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil)
	selection := router.Route("search and discover all references in docs")

	lower := strings.ToLower(selection.Rationale)
	if !strings.Contains(lower, "search") {
		t.Fatalf("rationale = %q, want matched keyword reference", selection.Rationale)
	}
}
