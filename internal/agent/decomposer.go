package agent

import (
	"regexp"
	"strings"
)

type decompositionPattern struct {
	Pattern  *regexp.Regexp
	Template string
}

var decompositionPatterns = []decompositionPattern{
	{regexp.MustCompile(`(?i)(create|implement|build).* (\d+|multiple|several|all) (component|file|module|feature)`), "parallel-files"},
	{regexp.MustCompile(`(?i)(refactor|migrate|update|upgrade).* (entire|all|whole|complete)`), "batch-operation"},
	{regexp.MustCompile(`(?i)(analyze|review|audit|examine).* (codebase|project|repository|entire)`), "phased-analysis"},
	{regexp.MustCompile(`(?i)(fix|resolve|debug).* (bug|issue|error|problem)`), "debug-fix"},
	{regexp.MustCompile(`(?i)(add|implement).* (feature|functionality|capability)`), "feature-implementation"},
}

func DecomposeTask(originalMessage string, complexity ComplexityScore, snapshot StateSnapshot) []DecomposedTask {
	// First try pattern-based decomposition
	for _, p := range decompositionPatterns {
		if p.Pattern.MatchString(originalMessage) {
			return GenerateSubtasksFromPattern(p.Template, originalMessage, snapshot)
		}
	}

	// Fallback: signal-based decomposition
	return GenerateSignalBasedSubtasks(complexity, snapshot)
}

func GenerateSubtasksFromPattern(template, message string, snapshot StateSnapshot) []DecomposedTask {
	switch template {
	case "parallel-files":
		return []DecomposedTask{
			{
				TaskID:         "phase-1-discover",
				AgentID:        "default",
				Message:        "List all files that need modification for: " + message + ". Return ONLY a JSON array of file paths. No other text.",
				AcceptanceCrit: []string{"Valid JSON array", "File paths are relative to workspace"},
				Produces:       []string{"file_list"},
				Priority:       1,
				TimeoutMS:      30000,
			},
			{
				TaskID:         "phase-2-implement",
				AgentID:        "default",
				Message:        "Implement the required changes for each file discovered in phase-1.",
				DependsOn:      []string{"phase-1-discover"},
				AcceptanceCrit: []string{"All files modified", "Changes compile/run"},
				Priority:       2,
				TimeoutMS:      120000,
			},
		}

	case "phased-analysis":
		return []DecomposedTask{
			{
				TaskID:         "phase-1-structure",
				AgentID:        "default",
				Message:        "Analyze project structure. Return: 1) Directory tree (top 3 levels), 2) Main entry points, 3) Tech stack detected.",
				AcceptanceCrit: []string{"Directory structure", "Entry points listed", "Tech stack identified"},
				Produces:       []string{"project_structure"},
				Priority:       1,
				TimeoutMS:      45000,
			},
			{
				TaskID:    "phase-2-deep",
				AgentID:   "default",
				Message:   "Deep analysis of core modules. Based on the structure from phase-1, analyze: architecture patterns, key dependencies, data flow.",
				DependsOn: []string{"phase-1-structure"},
				Produces:  []string{"architecture_analysis"},
				Priority:  2,
				TimeoutMS: 60000,
			},
			{
				TaskID:    "phase-3-report",
				AgentID:   "default",
				Message:   "Synthesize findings into a report covering: current state, risks, recommendations.",
				DependsOn: []string{"phase-2-deep"},
				Produces:  []string{"final_report"},
				Priority:  3,
				TimeoutMS: 30000,
			},
		}

	case "debug-fix":
		return []DecomposedTask{
			{
				TaskID:         "phase-1-diagnose",
				AgentID:        "default",
				Message:        "Diagnose the issue. Steps: 1) Reproduce the error, 2) Identify root cause, 3) List affected files. Return findings as structured JSON.",
				AcceptanceCrit: []string{"Error reproduced", "Root cause identified", "Affected files listed"},
				Produces:       []string{"diagnosis"},
				Priority:       1,
				TimeoutMS:      60000,
			},
			{
				TaskID:         "phase-2-fix",
				AgentID:        "default",
				Message:        "Implement the fix based on diagnosis. Verify the fix resolves the issue.",
				DependsOn:      []string{"phase-1-diagnose"},
				AcceptanceCrit: []string{"Fix implemented", "Error no longer occurs"},
				Priority:       2,
				TimeoutMS:      90000,
			},
		}

	default:
		return GenerateSignalBasedSubtasks(ComplexityScore{}, snapshot)
	}
}

func GenerateSignalBasedSubtasks(complexity ComplexityScore, snapshot StateSnapshot) []DecomposedTask {
	// Determine trigger type
	hasLoop := complexity.LoopScore >= 2
	hasFailure := complexity.FailureScore >= 2
	hasBlocked := complexity.BlockedScore >= 3
	hasContextPressure := complexity.ContextScore >= 2

	switch {
	case hasBlocked && hasLoop:
		// Blocked + looping: identify blockers and alternatives
		return []DecomposedTask{
			{
				TaskID:         "unblock-diagnose",
				AgentID:        "default",
				Message:        "Current execution is blocked. Analyze why: " + getFirstError(snapshot.LastErrorTypes) + ". Identify: 1) What's blocking, 2) Alternative approaches, 3) Missing inputs needed.",
				AcceptanceCrit: []string{"Blocker identified", "Alternatives proposed"},
				Priority:       1,
				TimeoutMS:      45000,
			},
			{
				TaskID:    "unblock-resolve",
				AgentID:   "default",
				Message:   "Based on diagnosis, implement the best alternative approach.",
				DependsOn: []string{"unblock-diagnose"},
				Priority:  2,
				TimeoutMS: 60000,
			},
		}

	case hasFailure && hasLoop:
		// Failure loop: diagnose + propose + implement
		return []DecomposedTask{
			{
				TaskID:         "failure-analyze",
				AgentID:        "default",
				Message:        "Analyze repeated failures. Tool: " + snapshot.LastToolAttempted + ". Error: " + strings.Join(snapshot.LastErrorTypes, ", ") + ". Propose a different approach.",
				AcceptanceCrit: []string{"Root cause found", "Alternative proposed"},
				Priority:       1,
				TimeoutMS:      30000,
			},
			{
				TaskID:    "failure-retry",
				AgentID:   "default",
				Message:   "Execute the proposed alternative approach.",
				DependsOn: []string{"failure-analyze"},
				Priority:  2,
				TimeoutMS: 60000,
			},
		}

	case hasContextPressure:
		// Context overflow: summarize + isolate next step
		return []DecomposedTask{
			{
				TaskID:         "context-summarize",
				AgentID:        "default",
				Message:        "Summarize current progress and state. Output: 1) What's been done, 2) What's pending, 3) Next atomic step.",
				AcceptanceCrit: []string{"Progress summarized", "Next step identified"},
				Produces:       []string{"progress_summary"},
				Priority:       1,
				TimeoutMS:      20000,
			},
			{
				TaskID:    "context-continue",
				AgentID:   "default",
				Message:   "Execute only the next atomic step identified in the summary. Do not attempt more.",
				DependsOn: []string{"context-summarize"},
				Priority:  2,
				TimeoutMS: 45000,
			},
		}

	default:
		// Generic fallback: simple diagnose + execute
		return []DecomposedTask{
			{
				TaskID:    "generic-assess",
				AgentID:   "default",
				Message:   "Assess current task state. Identify: 1) What needs to be done, 2) Smallest next step.",
				Priority:  1,
				TimeoutMS: 20000,
			},
			{
				TaskID:    "generic-execute",
				AgentID:   "default",
				Message:   "Execute the smallest next step identified.",
				DependsOn: []string{"generic-assess"},
				Priority:  2,
				TimeoutMS: 45000,
			},
		}
	}
}

func getFirstError(errors []string) string {
	if len(errors) > 0 {
		return errors[0]
	}
	return "unknown"
}
