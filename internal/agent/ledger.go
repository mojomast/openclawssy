package agent

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DecisionRecordTypeGoalInterpretation = "goal_interpretation"
	DecisionRecordTypeStrategySelection  = "strategy_selection"
	DecisionRecordTypeDelegationTrigger  = "delegation_trigger"
	DecisionRecordTypeRoleSelection      = "role_selection"
	DecisionRecordTypeToolDecision       = "tool_decision"
	DecisionRecordTypeConstraintActive   = "constraint_activation"
	DecisionRecordTypeTermination        = "termination"
)

type DecisionRecord struct {
	Timestamp    time.Time      `json:"timestamp"`
	RunID        string         `json:"run_id"`
	AgentID      string         `json:"agent_id"`
	RecordType   string         `json:"record_type"`
	Payload      map[string]any `json:"payload,omitempty"`
	HumanSummary string         `json:"human_summary"`
}

type DecisionPersistFunc func(ctx context.Context, record DecisionRecord) error

type DecisionLedger struct {
	mu      sync.RWMutex
	records map[string][]DecisionRecord
	persist DecisionPersistFunc
}

func NewDecisionLedger(persist DecisionPersistFunc) *DecisionLedger {
	return &DecisionLedger{
		records: make(map[string][]DecisionRecord),
		persist: persist,
	}
}

func (l *DecisionLedger) Record(ctx context.Context, record DecisionRecord) error {
	if l == nil {
		return nil
	}
	record = normalizeDecisionRecord(record)
	if record.RunID == "" || record.AgentID == "" || record.RecordType == "" {
		return nil
	}

	l.mu.Lock()
	l.records[record.RunID] = append(l.records[record.RunID], record)
	l.mu.Unlock()

	if l.persist != nil {
		return l.persist(ctx, record)
	}
	return nil
}

func (l *DecisionLedger) Records(runID string) []DecisionRecord {
	if l == nil {
		return nil
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}

	l.mu.RLock()
	records := append([]DecisionRecord(nil), l.records[runID]...)
	l.mu.RUnlock()

	for i := range records {
		records[i] = normalizeDecisionRecord(records[i])
	}
	sortDecisionRecords(records)
	return records
}

func normalizeDecisionRecord(record DecisionRecord) DecisionRecord {
	record.RunID = strings.TrimSpace(record.RunID)
	record.AgentID = strings.TrimSpace(record.AgentID)
	record.RecordType = strings.TrimSpace(record.RecordType)
	record.HumanSummary = strings.TrimSpace(record.HumanSummary)
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	} else {
		record.Timestamp = record.Timestamp.UTC()
	}
	if len(record.Payload) > 0 {
		payload := make(map[string]any, len(record.Payload))
		for k, v := range record.Payload {
			payload[k] = v
		}
		record.Payload = payload
	}
	return record
}

func sortDecisionRecords(records []DecisionRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Timestamp.Equal(records[j].Timestamp) {
			if records[i].RecordType == records[j].RecordType {
				return records[i].HumanSummary < records[j].HumanSummary
			}
			return records[i].RecordType < records[j].RecordType
		}
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
}
