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
				Message:        "Identify every workspace file that likely needs modification for this request: " + message + ". Return ONLY a JSON array of relative file paths, ordered by implementation priority. Do not include commentary or markdown.",
				AcceptanceCrit: []string{"Valid JSON array", "File paths are relative to workspace"},
				Produces:       []string{"file_list"},
				Priority:       1,
				TimeoutMS:      30000,
			},
			{
				TaskID:         "phase-2-implement",
				AgentID:        "default",
				Message:        "Implement the requested changes using the file list from phase-1. Modify only files required for the task, verify the changed behavior with the smallest relevant check, and report files changed plus any remaining risk.",
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
				Message:        "Analyze the project structure for the current task. Return only concise findings covering: 1) directory tree (top 3 levels max), 2) main entry points, 3) detected tech stack, 4) the most relevant modules to inspect next.",
				AcceptanceCrit: []string{"Directory structure", "Entry points listed", "Tech stack identified"},
				Produces:       []string{"project_structure"},
				Priority:       1,
				TimeoutMS:      45000,
			},
			{
				TaskID:    "phase-2-deep",
				AgentID:   "default",
				Message:   "Based on phase-1, analyze only the most relevant core modules. Explain architecture patterns, key dependencies, and main data flow, and call out the highest-risk or highest-leverage areas.",
				DependsOn: []string{"phase-1-structure"},
				Produces:  []string{"architecture_analysis"},
				Priority:  2,
				TimeoutMS: 60000,
			},
			{
				TaskID:    "phase-3-report",
				AgentID:   "default",
				Message:   "Synthesize the prior findings into a concise report covering current state, main risks, and specific recommendations in priority order.",
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
				Message:        "Diagnose the issue for this request: " + message + ". Steps: 1) reproduce or localize the failure, 2) identify the most likely root cause, 3) list affected files or systems, 4) propose the smallest credible fix. Return findings as structured JSON.",
				AcceptanceCrit: []string{"Error reproduced", "Root cause identified", "Affected files listed"},
				Produces:       []string{"diagnosis"},
				Priority:       1,
				TimeoutMS:      60000,
			},
			{
				TaskID:         "phase-2-fix",
				AgentID:        "default",
				Message:        "Implement the fix based on the diagnosis from phase-1. Prefer the smallest safe change, verify the issue is resolved with the most relevant check available, and report what changed plus any residual risk.",
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
				Message:        "Current execution is blocked. Analyze this blocker: " + getFirstError(snapshot.LastErrorTypes) + ". Identify: 1) what is blocking progress, 2) the best alternative approaches, 3) missing inputs or permissions, 4) the next action with the highest chance of success.",
				AcceptanceCrit: []string{"Blocker identified", "Alternatives proposed"},
				Priority:       1,
				TimeoutMS:      45000,
			},
			{
				TaskID:    "unblock-resolve",
				AgentID:   "default",
				Message:   "Based on the diagnosis, execute the best alternative approach. Prefer forward progress over repeated failed attempts, and report the result or the next concrete blocker.",
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
				Message:        "Analyze the repeated failure loop. Last tool: " + snapshot.LastToolAttempted + ". Recent errors: " + strings.Join(snapshot.LastErrorTypes, ", ") + ". Explain the likely root cause, why the current approach is failing, and propose a materially different next approach.",
				AcceptanceCrit: []string{"Root cause found", "Alternative proposed"},
				Priority:       1,
				TimeoutMS:      30000,
			},
			{
				TaskID:    "failure-retry",
				AgentID:   "default",
				Message:   "Execute the proposed alternative approach. Do not repeat the same failing action without a material change in method, inputs, or permissions.",
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
				Message:        "Summarize current progress under context pressure. Output only concise findings covering: 1) what has already been done, 2) what is still pending, 3) the single next atomic step with highest value.",
				AcceptanceCrit: []string{"Progress summarized", "Next step identified"},
				Produces:       []string{"progress_summary"},
				Priority:       1,
				TimeoutMS:      20000,
			},
			{
				TaskID:    "context-continue",
				AgentID:   "default",
				Message:   "Execute only the single next atomic step identified in the summary. Do not broaden scope, and stop after producing a concrete result for that step.",
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
				Message:   "Assess the current task state. Identify: 1) what still needs to be done, 2) the smallest next step that makes real progress, 3) any immediate blocker or assumption.",
				Priority:  1,
				TimeoutMS: 20000,
			},
			{
				TaskID:    "generic-execute",
				AgentID:   "default",
				Message:   "Execute the smallest next step identified in the assessment. Prefer concrete progress and report the exact result.",
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
