package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type alwaysFailSubAgentRunner struct {
	calls []DecomposedTask
}

func (m *alwaysFailSubAgentRunner) ExecuteSubAgent(_ context.Context, task DecomposedTask) (SubAgentOutput, error) {
	m.calls = append(m.calls, task)
	return SubAgentOutput{Success: false, Error: "boom"}, errors.New("boom")
}

func TestPlannerGeneratesTypedDecompositionPlan(t *testing.T) {
	tasks := []DecomposedTask{
		{
			TaskID:         "discover",
			AgentID:        "default",
			Message:        "search and discover relevant files for this bug",
			Produces:       []string{"file_list"},
			AcceptanceCrit: []string{"returns relevant file paths"},
			Priority:       1,
		},
		{
			TaskID:         "implement",
			AgentID:        "default",
			Message:        "write and implement the fix in the selected files",
			DependsOn:      []string{"discover"},
			Produces:       []string{"patch_summary"},
			AcceptanceCrit: []string{"fix compiles", "tests pass"},
			Priority:       2,
		},
	}

	plan, ordered, err := GenerateDecompositionPlan("failure loop and context pressure", DelegationModeSuggestOnly, tasks, nil)
	if err != nil {
		t.Fatalf("GenerateDecompositionPlan() error = %v", err)
	}
	if plan.DelegationMode != string(DelegationModeSuggestOnly) {
		t.Fatalf("plan.DelegationMode = %q, want %q", plan.DelegationMode, DelegationModeSuggestOnly)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 plan tasks, got %d", len(plan.Tasks))
	}
	for _, node := range plan.Tasks {
		if strings.TrimSpace(node.TaskID) == "" {
			t.Fatal("expected task_id to be populated")
		}
		if strings.TrimSpace(node.Description) == "" {
			t.Fatal("expected description to be populated")
		}
		if strings.TrimSpace(node.AssignedRole) == "" {
			t.Fatal("expected assigned_role to be populated")
		}
		if node.Confidence <= 0 {
			t.Fatalf("expected confidence > 0 for task %q, got %f", node.TaskID, node.Confidence)
		}
		if strings.TrimSpace(node.Rationale) == "" {
			t.Fatalf("expected rationale for task %q", node.TaskID)
		}
	}
	if len(plan.DependencyDAG) != 1 {
		t.Fatalf("expected 1 dependency edge, got %d", len(plan.DependencyDAG))
	}
	if plan.DependencyDAG[0].FromTaskID != "discover" || plan.DependencyDAG[0].ToTaskID != "implement" {
		t.Fatalf("unexpected dependency edge: %#v", plan.DependencyDAG[0])
	}

	idx := map[string]int{}
	for i, task := range ordered {
		idx[task.TaskID] = i
	}
	if idx["discover"] >= idx["implement"] {
		t.Fatalf("expected dependency ordering discover < implement, got %d >= %d", idx["discover"], idx["implement"])
	}
}

func TestDelegationModeSuggestOnlyReturnsPlanWithoutExecution(t *testing.T) {
	failCmd := []byte(`{"command":"bash","args":["-lc","deploy --target=prod"]}`)
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: failCmd}}, PromptTokens: 0},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: failCmd}}, PromptTokens: 112000},
		{FinalText: "BUG: should not reach final model response"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "exit status 1", Output: "permission denied"},
		"2": {ID: "2", Error: "exit status 1", Output: "permission denied"},
	}}
	subRunner := &mockSubAgentRunner{result: SubAgentOutput{RunID: "sub-1", FinalText: "done", Success: true}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20, SubAgentRunner: subRunner}
	out, err := runner.Run(context.Background(), RunInput{
		Message:        "deploy the application to production",
		DelegationMode: string(DelegationModeSuggestOnly),
		AutoDelegate:   true,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.DecompositionPlan == nil {
		t.Fatal("expected decomposition_plan to be populated")
	}
	if len(subRunner.calls) != 0 {
		t.Fatalf("expected no subagent execution in suggest_only mode, got %d calls", len(subRunner.calls))
	}
	if !strings.Contains(out.FinalText, "Delegation Plan") {
		t.Fatalf("expected plan output in final text, got %q", out.FinalText)
	}
	if len(model.reqs) != 2 {
		t.Fatalf("expected trigger to return plan before third model call, got %d requests", len(model.reqs))
	}
}

func TestDelegationModeApprovePlanHonorsApproval(t *testing.T) {
	makeRunner := func() (Runner, *mockModel, *mockSubAgentRunner) {
		failCmd := []byte(`{"command":"bash","args":["-lc","deploy --target=prod"]}`)
		model := &mockModel{responses: []ModelResponse{
			{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: failCmd}}, PromptTokens: 0},
			{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: failCmd}}, PromptTokens: 112000},
			{FinalText: "BUG: should not reach final model response"},
		}}
		tools := &mockTools{results: map[string]ToolCallResult{
			"1": {ID: "1", Error: "exit status 1", Output: "permission denied"},
			"2": {ID: "2", Error: "exit status 1", Output: "permission denied"},
		}}
		subRunner := &mockSubAgentRunner{result: SubAgentOutput{RunID: "sub-1", FinalText: "approved delegation done", Success: true}}
		return Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20, SubAgentRunner: subRunner}, model, subRunner
	}

	t.Run("waits_for_approval", func(t *testing.T) {
		runner, _, subRunner := makeRunner()
		out, err := runner.Run(context.Background(), RunInput{
			Message:            "deploy the application to production",
			DelegationMode:     string(DelegationModeApprovePlan),
			DelegationApproved: false,
		})
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if out.DecompositionPlan == nil {
			t.Fatal("expected decomposition_plan to be populated")
		}
		if len(subRunner.calls) != 0 {
			t.Fatalf("expected no execution before approval, got %d calls", len(subRunner.calls))
		}
		if !strings.Contains(strings.ToLower(out.FinalText), "requires operator approval") {
			t.Fatalf("expected approval wait message, got %q", out.FinalText)
		}
	})

	t.Run("executes_when_approved", func(t *testing.T) {
		runner, _, subRunner := makeRunner()
		out, err := runner.Run(context.Background(), RunInput{
			Message:            "deploy the application to production",
			DelegationMode:     string(DelegationModeApprovePlan),
			DelegationApproved: true,
		})
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if len(subRunner.calls) == 0 {
			t.Fatal("expected subagent execution after approval")
		}
		if !strings.Contains(out.FinalText, "approved delegation done") {
			t.Fatalf("expected delegated output in final text, got %q", out.FinalText)
		}
	})
}

func TestDelegationModeAutoTrustedOnlyExecutesForTrustedPlan(t *testing.T) {
	auto, summary := shouldAutoExecutePlan(DelegationModeAutoTrusted, DecompositionPlan{AllRolesBuiltIn: true, MinConfidence: 0.71}, false)
	if !auto {
		t.Fatalf("expected trusted plan to auto execute, summary=%q", summary)
	}
	auto, summary = shouldAutoExecutePlan(DelegationModeAutoTrusted, DecompositionPlan{AllRolesBuiltIn: false, MinConfidence: 0.95}, false)
	if auto {
		t.Fatalf("expected non-built-in role plan to be withheld, summary=%q", summary)
	}
	auto, summary = shouldAutoExecutePlan(DelegationModeAutoTrusted, DecompositionPlan{AllRolesBuiltIn: true, MinConfidence: 0.70}, false)
	if auto {
		t.Fatalf("expected confidence threshold to require >0.7, summary=%q", summary)
	}
}

func TestDelegationModeFullAutoKeepsPromptOnlyTriggerAdvisory(t *testing.T) {
	// Use different paths so per-path repetition stays at 1 (LoopScore=0).
	// Two consecutive failures still give FailureScore=2 → TotalScore=2
	// → Moderate → PromptOnly trigger, which should stay advisory.
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.read", Arguments: []byte(`{"path":"notes.txt"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.read", Arguments: []byte(`{"path":"todo.txt"}`)}}},
		{FinalText: "finished without planner takeover"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "tool_execution_failed (fs.read): open notes.txt: no such file or directory"},
		"2": {ID: "2", Error: "tool_execution_failed (fs.read): open todo.txt: no such file or directory"},
	}}
	subRunner := &mockSubAgentRunner{result: SubAgentOutput{RunID: "sub-1", FinalText: "should not run", Success: true}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20, SubAgentRunner: subRunner}
	out, err := runner.Run(context.Background(), RunInput{
		Message:        "check the missing notes file",
		DelegationMode: string(DelegationModeFullAuto),
		AutoDelegate:   true,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "finished without planner takeover" {
		t.Fatalf("expected direct completion, got %q", out.FinalText)
	}
	if len(subRunner.calls) != 0 {
		t.Fatalf("expected no subagent execution for advisory trigger, got %d calls", len(subRunner.calls))
	}
	if len(model.reqs) != 3 {
		t.Fatalf("expected third model turn to complete directly, got %d requests", len(model.reqs))
	}
	if !strings.Contains(model.reqs[2].SystemPrompt, "DELEGATION_RECOMMENDED") {
		t.Fatalf("expected advisory delegation hint in prompt, got %q", model.reqs[2].SystemPrompt)
	}
}

type sequentialSubAgentRunner struct {
	calls   []DecomposedTask
	results []SubAgentOutput
	index   int
}

func (r *sequentialSubAgentRunner) ExecuteSubAgent(_ context.Context, task DecomposedTask) (SubAgentOutput, error) {
	r.calls = append(r.calls, task)
	if r.index >= len(r.results) {
		return SubAgentOutput{Success: false, Error: "missing sequential result"}, errors.New("missing sequential result")
	}
	result := r.results[r.index]
	r.index++
	return result, nil
}

func TestExecuteDelegatedTasksInjectsDependencyContextFromTaskResult(t *testing.T) {
	subRunner := &sequentialSubAgentRunner{results: []SubAgentOutput{
		{RunID: "sub-1", FinalText: "root cause is missing config", Success: true},
		{RunID: "sub-2", FinalText: "fixed configuration", Success: true},
	}}
	runner := Runner{SubAgentRunner: subRunner}
	state := newRunState(RunInput{Message: "fix the config issue", RunID: "parent-run"}, runner)
	state.delegationReason = "failures>=2"

	tasks := []DecomposedTask{
		{
			TaskID:   "generic-assess",
			AgentID:  "default",
			Message:  "Assess the current task state.",
			Priority: 1,
		},
		{
			TaskID:    "generic-execute",
			AgentID:   "default",
			Message:   "Execute the smallest next step identified in the assessment.",
			DependsOn: []string{"generic-assess"},
			Priority:  2,
		},
	}

	if err := runner.executeDelegatedTasks(context.Background(), state, tasks, RunInput{RunID: "parent-run", Message: "fix the config issue"}); err != nil {
		t.Fatalf("executeDelegatedTasks() error = %v", err)
	}
	if len(subRunner.calls) != 2 {
		t.Fatalf("expected 2 subagent calls, got %d", len(subRunner.calls))
	}
	if !strings.Contains(subRunner.calls[1].Message, "Context from previous steps") {
		t.Fatalf("expected dependency context in second task, got %q", subRunner.calls[1].Message)
	}
	if !strings.Contains(subRunner.calls[1].Message, "generic-assess: root cause is missing config") {
		t.Fatalf("expected first task result injected into dependency context, got %q", subRunner.calls[1].Message)
	}
}

func TestDelegationFailureCascadingRetriesEscalatesAndRecordsFailure(t *testing.T) {
	subRunner := &alwaysFailSubAgentRunner{}
	runner := Runner{SubAgentRunner: subRunner}
	state := newRunState(RunInput{Message: "delegate task"}, runner)
	state.delegationReason = "forced due repeated failures"
	state.out.DecompositionPlan = &DecompositionPlan{DelegationMode: string(DelegationModeFullAuto)}

	tasks := []DecomposedTask{
		{
			TaskID:            "task-1",
			AgentID:           "default",
			Message:           "implement and patch the file",
			AssignedRole:      "implementer",
			RoutingConfidence: 0.4,
			RoutingRationale:  "low-confidence routing",
			Priority:          1,
		},
		{
			TaskID:            "task-2",
			AgentID:           "default",
			Message:           "verify the patch",
			AssignedRole:      "verifier",
			RoutingConfidence: 0.9,
			RoutingRationale:  "verification role selected",
			DependsOn:         []string{"task-1"},
			Priority:          2,
		},
	}

	err := runner.executeDelegatedTasks(context.Background(), state, tasks, RunInput{DelegationMode: string(DelegationModeFullAuto)})
	if err != nil {
		t.Fatalf("executeDelegatedTasks() error = %v", err)
	}
	if len(subRunner.calls) != 2 {
		t.Fatalf("expected retry with same role (2 calls), got %d", len(subRunner.calls))
	}
	if !strings.Contains(state.completedSubtasks["task-1"], "FAILED: escalated") {
		t.Fatalf("expected task-1 escalation failure, got %q", state.completedSubtasks["task-1"])
	}
	if !strings.Contains(state.completedSubtasks["task-2"], "FAILED: dependency task-1 failed") {
		t.Fatalf("expected task-2 dependency cascade failure, got %q", state.completedSubtasks["task-2"])
	}

	seenOutcome := map[string]bool{}
	for _, event := range state.out.DelegationEvents {
		if strings.TrimSpace(event.Outcome) != "" {
			seenOutcome[event.Outcome] = true
		}
		if event.Outcome == "retry_same_role" || event.Outcome == "escalated" || event.Outcome == "failed_dependency" {
			if strings.TrimSpace(event.TriggerReason) == "" {
				t.Fatalf("expected trigger_reason on event %+v", event)
			}
			if strings.TrimSpace(event.SelectedRole) == "" {
				t.Fatalf("expected selected_role on event %+v", event)
			}
			if strings.TrimSpace(event.TaskAssignment) == "" {
				t.Fatalf("expected task_assignment on event %+v", event)
			}
			if strings.TrimSpace(event.Rationale) == "" {
				t.Fatalf("expected rationale on event %+v", event)
			}
		}
	}
	for _, outcome := range []string{"retry_same_role", "escalated", "failed_dependency"} {
		if !seenOutcome[outcome] {
			t.Fatalf("expected delegation event outcome %q, outcomes=%v", outcome, seenOutcome)
		}
	}
}
