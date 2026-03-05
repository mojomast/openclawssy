package agent

import (
	"strings"
	"testing"
)

// TestDecomposeTaskParallelFilesPattern verifies that a message about creating
// multiple component files matches the "parallel-files" decomposition pattern
// and returns exactly 2 subtasks: discover + implement.
func TestDecomposeTaskParallelFilesPattern(t *testing.T) {
	msg := "create 5 component files for the dashboard"
	tasks := DecomposeTask(msg, ComplexityScore{}, StateSnapshot{})

	if len(tasks) != 2 {
		t.Fatalf("expected 2 subtasks for parallel-files pattern, got %d", len(tasks))
	}

	// First task: discover phase
	discover := tasks[0]
	if discover.TaskID != "phase-1-discover" {
		t.Fatalf("expected first task ID 'phase-1-discover', got %q", discover.TaskID)
	}
	if !strings.Contains(strings.ToLower(discover.Message), "list all files") {
		t.Fatalf("expected discover task to mention listing files, got %q", discover.Message)
	}
	if len(discover.Produces) == 0 || discover.Produces[0] != "file_list" {
		t.Fatalf("expected discover task to produce 'file_list', got %v", discover.Produces)
	}
	if len(discover.DependsOn) != 0 {
		t.Fatalf("expected discover task to have no dependencies, got %v", discover.DependsOn)
	}
	if discover.Priority != 1 {
		t.Fatalf("expected discover priority 1, got %d", discover.Priority)
	}

	// Second task: implement phase
	impl := tasks[1]
	if impl.TaskID != "phase-2-implement" {
		t.Fatalf("expected second task ID 'phase-2-implement', got %q", impl.TaskID)
	}
	if len(impl.DependsOn) != 1 || impl.DependsOn[0] != "phase-1-discover" {
		t.Fatalf("expected implement task to depend on 'phase-1-discover', got %v", impl.DependsOn)
	}
	if impl.Priority != 2 {
		t.Fatalf("expected implement priority 2, got %d", impl.Priority)
	}
}

// TestDecomposeTaskDebugFixPattern verifies that a message about fixing a bug
// matches the "debug-fix" pattern and returns 2 subtasks: diagnose + fix.
func TestDecomposeTaskDebugFixPattern(t *testing.T) {
	msg := "fix the bug in authentication"
	tasks := DecomposeTask(msg, ComplexityScore{}, StateSnapshot{})

	if len(tasks) != 2 {
		t.Fatalf("expected 2 subtasks for debug-fix pattern, got %d", len(tasks))
	}

	// First task: diagnose phase
	diagnose := tasks[0]
	if diagnose.TaskID != "phase-1-diagnose" {
		t.Fatalf("expected first task ID 'phase-1-diagnose', got %q", diagnose.TaskID)
	}
	if !strings.Contains(strings.ToLower(diagnose.Message), "diagnose") {
		t.Fatalf("expected diagnose task message to mention diagnosing, got %q", diagnose.Message)
	}
	if len(diagnose.Produces) == 0 || diagnose.Produces[0] != "diagnosis" {
		t.Fatalf("expected diagnose task to produce 'diagnosis', got %v", diagnose.Produces)
	}
	if len(diagnose.DependsOn) != 0 {
		t.Fatalf("expected diagnose task to have no dependencies, got %v", diagnose.DependsOn)
	}
	if diagnose.Priority != 1 {
		t.Fatalf("expected diagnose priority 1, got %d", diagnose.Priority)
	}

	// Second task: fix phase
	fix := tasks[1]
	if fix.TaskID != "phase-2-fix" {
		t.Fatalf("expected second task ID 'phase-2-fix', got %q", fix.TaskID)
	}
	if len(fix.DependsOn) != 1 || fix.DependsOn[0] != "phase-1-diagnose" {
		t.Fatalf("expected fix task to depend on 'phase-1-diagnose', got %v", fix.DependsOn)
	}
	if fix.Priority != 2 {
		t.Fatalf("expected fix priority 2, got %d", fix.Priority)
	}
	if !strings.Contains(strings.ToLower(fix.Message), "fix") {
		t.Fatalf("expected fix task message to mention fixing, got %q", fix.Message)
	}
}

// TestDecomposeTaskFallbackWhenNoPattern verifies that a message that doesn't
// match any pattern falls through to signal-based subtask generation. With
// a zero ComplexityScore (no stuck signals), the default fallback produces
// generic-assess + generic-execute tasks.
func TestDecomposeTaskFallbackWhenNoPattern(t *testing.T) {
	msg := "do something random"
	tasks := DecomposeTask(msg, ComplexityScore{}, StateSnapshot{})

	if len(tasks) < 2 {
		t.Fatalf("expected at least 2 fallback subtasks, got %d", len(tasks))
	}

	// The default signal-based fallback (no stuck signals) should produce
	// "generic-assess" and "generic-execute".
	assess := tasks[0]
	if assess.TaskID != "generic-assess" {
		t.Fatalf("expected first fallback task ID 'generic-assess', got %q", assess.TaskID)
	}
	if assess.Priority != 1 {
		t.Fatalf("expected assess priority 1, got %d", assess.Priority)
	}

	execute := tasks[1]
	if execute.TaskID != "generic-execute" {
		t.Fatalf("expected second fallback task ID 'generic-execute', got %q", execute.TaskID)
	}
	if len(execute.DependsOn) != 1 || execute.DependsOn[0] != "generic-assess" {
		t.Fatalf("expected execute to depend on 'generic-assess', got %v", execute.DependsOn)
	}
	if execute.Priority != 2 {
		t.Fatalf("expected execute priority 2, got %d", execute.Priority)
	}
}
