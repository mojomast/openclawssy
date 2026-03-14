package promptstack

import (
	"strings"
	"testing"
)

func TestLintDuplicateInstructions(t *testing.T) {
	t.Parallel()

	t.Run("flags duplicate instruction across layers", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.GlobalOperatorPolicy.Content = "Always verify changes with tests before finishing."
		stack.AgentIdentity.Content = "Always verify changes with tests before finishing."

		issues := NewLinter(nil).Lint(stack)
		if !hasLintIssue(issues, LintSeverityWarning, LayerAgentIdentity, "Duplicate instruction") {
			t.Fatalf("expected duplicate instruction warning, got %#v", issues)
		}
	})

	t.Run("does not flag unique instructions", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.GlobalOperatorPolicy.Content = "Protect secrets in all outputs."
		stack.AgentIdentity.Content = "Use concise, direct language."

		issues := NewLinter(nil).Lint(stack)
		if hasIssueContaining(issues, "Duplicate instruction") {
			t.Fatalf("did not expect duplicate instruction warning, got %#v", issues)
		}
	})
}

func TestLintConflictingToolDirectives(t *testing.T) {
	t.Parallel()

	t.Run("flags allow/deny conflict for same tool", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.ToolSafetyRules.Content = "Allow tool fs.read."
		stack.SessionOverlay.Content += "\nDeny tool fs.read."

		issues := NewLinter(nil).Lint(stack)
		if !hasLintIssue(issues, LintSeverityError, LayerSessionOverlay, "Conflicting tool-use directives") {
			t.Fatalf("expected tool conflict issue, got %#v", issues)
		}
	})

	t.Run("does not flag non-conflicting tool directives", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.ToolSafetyRules.Content = "Allow tool fs.read."
		stack.SessionOverlay.Content += "\nDeny tool shell.exec."

		issues := NewLinter(nil).Lint(stack)
		if hasIssueContaining(issues, "Conflicting tool-use directives") {
			t.Fatalf("did not expect tool conflict issue, got %#v", issues)
		}
	})
}

func TestLintVagueDelegationLanguage(t *testing.T) {
	t.Parallel()

	t.Run("flags vague delegation phrase without trigger", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.DelegationPolicy.Content = "Maybe delegate this work to another role."

		issues := NewLinter(nil).Lint(stack)
		if !hasLintIssue(issues, LintSeverityWarning, LayerDelegationPolicy, "Vague delegation language") {
			t.Fatalf("expected vague delegation warning, got %#v", issues)
		}
	})

	t.Run("does not flag delegation phrase with clear trigger", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.DelegationPolicy.Content = "Consider delegating when implementation exceeds two files."

		issues := NewLinter(nil).Lint(stack)
		if hasIssueContaining(issues, "Vague delegation language") {
			t.Fatalf("did not expect vague delegation warning, got %#v", issues)
		}
	})
}

func TestLintUndefinedRoleReferences(t *testing.T) {
	t.Parallel()

	t.Run("flags role reference not in defined role list", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.DelegationPolicy.Content = "Delegate to analyst when a design review is needed."

		issues := NewLinter([]string{"planner", "implementer"}).Lint(stack)
		if !hasLintIssue(issues, LintSeverityError, LayerDelegationPolicy, "Undefined role reference") {
			t.Fatalf("expected undefined role issue, got %#v", issues)
		}
	})

	t.Run("does not flag defined role reference", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.DelegationPolicy.Content = "Delegate to planner when decomposition is required."

		issues := NewLinter([]string{"planner", "implementer"}).Lint(stack)
		if hasIssueContaining(issues, "Undefined role reference") {
			t.Fatalf("did not expect undefined role issue, got %#v", issues)
		}
	})
}

func TestLintMissingSuccessCriteria(t *testing.T) {
	t.Parallel()

	t.Run("flags missing success criteria", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.SessionOverlay.Content = "Stop and return when all requested steps are complete."

		issues := NewLinter(nil).Lint(stack)
		if !hasLintIssue(issues, LintSeverityWarning, LintLayerAll, "Missing success criteria") {
			t.Fatalf("expected missing success criteria warning, got %#v", issues)
		}
	})

	t.Run("does not flag when success criteria is present", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()

		issues := NewLinter(nil).Lint(stack)
		if hasIssueContaining(issues, "Missing success criteria") {
			t.Fatalf("did not expect missing success criteria warning, got %#v", issues)
		}
	})
}

func TestLintAbsentTerminationRules(t *testing.T) {
	t.Parallel()

	t.Run("flags missing termination rules", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()
		stack.SessionOverlay.Content = "Success criteria: task is complete when all requested checks pass."

		issues := NewLinter(nil).Lint(stack)
		if !hasLintIssue(issues, LintSeverityWarning, LintLayerAll, "Absent termination rules") {
			t.Fatalf("expected absent termination rules warning, got %#v", issues)
		}
	})

	t.Run("does not flag when termination rules are present", func(t *testing.T) {
		t.Parallel()

		stack := lintBaselineStack()

		issues := NewLinter(nil).Lint(stack)
		if hasIssueContaining(issues, "Absent termination rules") {
			t.Fatalf("did not expect absent termination rules warning, got %#v", issues)
		}
	})
}

func TestLintCleanPromptReturnsNoIssues(t *testing.T) {
	t.Parallel()

	stack := lintBaselineStack()
	issues := NewLinter(nil).Lint(stack)
	if len(issues) != 0 {
		t.Fatalf("expected no issues for clean prompt, got %#v", issues)
	}
}

func lintBaselineStack() PromptStack {
	stack := NewPromptStack()
	stack.GlobalOperatorPolicy.Content = "Protect secrets and follow workspace boundaries."
	stack.AgentIdentity.Content = "Act as a precise implementation partner."
	stack.ToolSafetyRules.Content = "Allow tool fs.read for source inspection."
	stack.DelegationPolicy.Content = "Delegate to planner when task decomposition is required."
	stack.SessionOverlay.Content = "Success criteria: task is complete when all requested checks pass.\nStop and return once completion criteria are satisfied."
	return stack
}

func hasLintIssue(issues []LintIssue, severity LintSeverity, layerID, descriptionSnippet string) bool {
	for _, issue := range issues {
		if issue.Severity != severity {
			continue
		}
		if issue.LayerID != layerID {
			continue
		}
		if !strings.Contains(issue.Description, descriptionSnippet) {
			continue
		}
		if strings.TrimSpace(issue.SuggestedFix) == "" {
			return false
		}
		return true
	}
	return false
}

func hasIssueContaining(issues []LintIssue, descriptionSnippet string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Description, descriptionSnippet) {
			return true
		}
	}
	return false
}
