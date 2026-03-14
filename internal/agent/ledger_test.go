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
