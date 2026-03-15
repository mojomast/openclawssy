package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLedgerStoresChronologicalRecordsByRun(t *testing.T) {
	ledger := NewDecisionLedger(nil)
	now := time.Now().UTC()

	if err := ledger.Record(context.Background(), DecisionRecord{
		Timestamp:    now.Add(2 * time.Second),
		RunID:        "run-a",
		AgentID:      "default",
		RecordType:   DecisionRecordTypeTermination,
		HumanSummary: "done",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := ledger.Record(context.Background(), DecisionRecord{
		Timestamp:    now,
		RunID:        "run-a",
		AgentID:      "default",
		RecordType:   DecisionRecordTypeGoalInterpretation,
		HumanSummary: "goal",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := ledger.Record(context.Background(), DecisionRecord{
		Timestamp:    now,
		RunID:        "run-b",
		AgentID:      "worker",
		RecordType:   DecisionRecordTypeGoalInterpretation,
		HumanSummary: "other run",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	records := ledger.Records("run-a")
	if len(records) != 2 {
		t.Fatalf("expected 2 records for run-a, got %d", len(records))
	}
	if records[0].RecordType != DecisionRecordTypeGoalInterpretation {
		t.Fatalf("expected first record to be goal interpretation, got %q", records[0].RecordType)
	}
	if records[1].RecordType != DecisionRecordTypeTermination {
		t.Fatalf("expected second record to be termination, got %q", records[1].RecordType)
	}

	other := ledger.Records("run-b")
	if len(other) != 1 || other[0].AgentID != "worker" {
		t.Fatalf("unexpected records for run-b: %#v", other)
	}
}

func TestLedgerRunnerEmitsKeyDecisionRecords(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{
			PromptTokens: 110000,
			ToolCalls: []ToolCallRequest{{
				ID:        "call-1",
				Name:      "fs.list",
				Arguments: []byte(`{"path":"."}`),
			}},
		},
	}}
	tools := &mockTools{}
	ledger := NewDecisionLedger(nil)

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}
	_, err := runner.Run(context.Background(), RunInput{
		AgentID:        "default",
		RunID:          "run-ledger-key-events",
		Message:        "Investigate and implement a change with delegation if needed.",
		AllowedTools:   []string{"fs.list", "agent.list", "agent.run"},
		DelegationMode: string(DelegationModeSuggestOnly),
		DecisionLedger: ledger,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	records := ledger.Records("run-ledger-key-events")
	if len(records) == 0 {
		t.Fatal("expected decision records")
	}

	required := map[string]bool{
		DecisionRecordTypeGoalInterpretation: false,
		DecisionRecordTypeStrategySelection:  false,
		DecisionRecordTypeDelegationTrigger:  false,
		DecisionRecordTypeRoleSelection:      false,
		DecisionRecordTypeToolDecision:       false,
		DecisionRecordTypeConstraintActive:   false,
		DecisionRecordTypeTermination:        false,
	}
	for _, record := range records {
		if _, ok := required[record.RecordType]; ok {
			required[record.RecordType] = true
		}
	}
	for recordType, seen := range required {
		if !seen {
			t.Fatalf("expected decision record type %q in %+v", recordType, records)
		}
	}

	delegationRecords := make([]DecisionRecord, 0)
	for _, record := range records {
		if record.RecordType == DecisionRecordTypeDelegationTrigger {
			delegationRecords = append(delegationRecords, record)
			assertDelegationTriggerPayloadContract(t, record, "run-ledger-key-events")
		}
	}
	if len(delegationRecords) == 0 {
		t.Fatalf("expected delegation trigger records, got %+v", records)
	}

	triggerFiredRecordFound := false
	for _, record := range delegationRecords {
		if _, hasMode := record.Payload["mode"]; hasMode {
			triggerFiredRecordFound = true
			break
		}
	}
	if !triggerFiredRecordFound {
		t.Fatalf("expected at least one trigger-fired delegation record with mode metadata, got %+v", delegationRecords)
	}
}

type denyToolExecutor struct{}

func (denyToolExecutor) Execute(_ context.Context, call ToolCallRequest) (ToolCallResult, error) {
	return ToolCallResult{ID: call.ID}, errors.New("policy.denied (fs.write): capability denied: agent=\"default\" tool=\"fs.write\"")
}

func TestLedgerToolDeniedDecisionIncludesPolicyReference(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "call-1", Name: "fs.write", Arguments: []byte(`{"path":"x.txt","content":"x"}`)}}},
		{FinalText: "done"},
	}}
	ledger := NewDecisionLedger(nil)
	runner := Runner{Model: model, ToolExecutor: denyToolExecutor{}, MaxToolIterations: 4}

	_, err := runner.Run(context.Background(), RunInput{
		AgentID:        "default",
		RunID:          "run-ledger-policy-deny",
		Message:        "write a file",
		AllowedTools:   []string{"fs.write"},
		DecisionLedger: ledger,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	records := ledger.Records("run-ledger-policy-deny")
	deniedFound := false
	for _, record := range records {
		if record.RecordType != DecisionRecordTypeToolDecision {
			continue
		}
		decision, _ := record.Payload["decision"].(string)
		policyRef, _ := record.Payload["policy_reference"].(string)
		if decision == "denied" && strings.TrimSpace(policyRef) != "" {
			deniedFound = true
			break
		}
	}
	if !deniedFound {
		t.Fatalf("expected denied tool decision with policy reference, got %+v", records)
	}
}

type integrationSubAgentRunner struct {
	outputs map[string]SubAgentOutput
}

func (r integrationSubAgentRunner) ExecuteSubAgent(_ context.Context, input DecomposedTask) (SubAgentOutput, error) {
	if out, ok := r.outputs[strings.TrimSpace(input.TaskID)]; ok {
		return out, nil
	}
	return SubAgentOutput{RunID: "subagent-default", FinalText: "ok", Success: true}, nil
}

func TestIntegrationDelegationEventsEmitLedgerRecordsWithRoutingContext(t *testing.T) {
	tasks := []DecomposedTask{
		{
			TaskID:                "task-1",
			AgentID:               "worker",
			Message:               "Read repository state and summarize",
			AssignedRole:          "scout",
			RoutingConfidence:     0.93,
			RoutingRationale:      "Read-only reconnaissance task",
			RoleAllowedTools:      []string{"fs.read", "fs.search"},
			RoleMaxToolIterations: 12,
			RoleTimeoutMS:         120000,
			DependsOn:             nil,
			Produces:              []string{"scan_summary"},
		},
	}

	ledger := NewDecisionLedger(nil)
	runner := Runner{
		SubAgentRunner: integrationSubAgentRunner{
			outputs: map[string]SubAgentOutput{
				"task-1": {RunID: "sub-run-1", FinalText: "scan complete", Success: true},
			},
		},
	}
	input := RunInput{
		RunID:          "run-integration-ledger",
		AgentID:        "default",
		DecisionLedger: ledger,
	}
	state := newRunState(input, runner)
	state.delegationReason = "task requires decomposition"

	if err := runner.executeDelegatedTasks(context.Background(), state, tasks, input); err != nil {
		t.Fatalf("executeDelegatedTasks() error = %v", err)
	}

	if len(state.out.DelegationEvents) == 0 {
		t.Fatal("expected delegation events to be recorded in run output")
	}

	records := ledger.Records("run-integration-ledger")
	if len(records) == 0 {
		t.Fatal("expected decision ledger records for run")
	}

	delegationRecords := make([]DecisionRecord, 0)
	for _, record := range records {
		if record.RecordType == DecisionRecordTypeDelegationTrigger {
			delegationRecords = append(delegationRecords, record)
		}
	}
	if len(delegationRecords) == 0 {
		t.Fatalf("expected delegation-trigger ledger records, got %+v", records)
	}

	for _, event := range state.out.DelegationEvents {
		matched := false
		for _, record := range delegationRecords {
			assertDelegationTriggerPayloadContract(t, record, "run-integration-ledger")

			taskID, _ := record.Payload["task_id"].(string)
			outcome, _ := record.Payload["outcome"].(string)
			if strings.TrimSpace(taskID) != strings.TrimSpace(event.TaskID) || strings.TrimSpace(outcome) != strings.TrimSpace(event.Outcome) {
				continue
			}

			triggerReason, _ := record.Payload["trigger_reason"].(string)
			selectedRole, _ := record.Payload["selected_role"].(string)
			taskAssignment, _ := record.Payload["task_assignment"].(string)
			parentRunID, _ := record.Payload["parent_run_id"].(string)

			if strings.TrimSpace(triggerReason) == "" {
				t.Fatalf("expected trigger_reason in payload, got %+v", record.Payload)
			}
			if strings.TrimSpace(selectedRole) == "" {
				t.Fatalf("expected selected_role in payload, got %+v", record.Payload)
			}
			if strings.TrimSpace(taskAssignment) == "" {
				t.Fatalf("expected task_assignment in payload, got %+v", record.Payload)
			}
			if parentRunID != "run-integration-ledger" {
				t.Fatalf("expected parent_run_id run-integration-ledger, got %q", parentRunID)
			}

			matched = true
			break
		}
		if !matched {
			t.Fatalf("missing delegation ledger record for event %+v", event)
		}
	}
}

func assertDelegationTriggerPayloadContract(t *testing.T, record DecisionRecord, expectedParentRunID string) {
	t.Helper()

	if record.RecordType != DecisionRecordTypeDelegationTrigger {
		t.Fatalf("assertDelegationTriggerPayloadContract called for non-delegation record: %+v", record)
	}

	payload := record.Payload
	if payload == nil {
		t.Fatalf("expected delegation payload, got nil for record %+v", record)
	}

	requiredNonEmpty := []string{"trigger_reason", "selected_role", "task_assignment", "parent_run_id"}
	for _, key := range requiredNonEmpty {
		value, ok := payload[key]
		if !ok {
			t.Fatalf("expected delegation payload key %q, got %+v", key, payload)
		}
		text, ok := value.(string)
		if !ok {
			t.Fatalf("expected delegation payload key %q to be string, got %T (%#v)", key, value, value)
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("expected delegation payload key %q to be non-empty, got %+v", key, payload)
		}
	}

	if got := strings.TrimSpace(payload["parent_run_id"].(string)); got != expectedParentRunID {
		t.Fatalf("expected parent_run_id=%q, got %q in payload %+v", expectedParentRunID, got, payload)
	}

	if _, ok := payload["confidence"]; !ok {
		t.Fatalf("expected delegation payload key confidence, got %+v", payload)
	}
	if _, ok := floatValueFromAny(payload["confidence"]); !ok {
		t.Fatalf("expected confidence numeric, got %T (%#v)", payload["confidence"], payload["confidence"])
	}
}
