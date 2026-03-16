package httpchannel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"openclawssy/internal/messagecontent"
)

var ErrRunNotFound = errors.New("run not found")

type Run struct {
	ID                string                `json:"id"`
	InstanceID        string                `json:"instance_id,omitempty"`
	AgentID           string                `json:"agent_id"`
	Message           string                `json:"message"`
	ContentParts      []messagecontent.Part `json:"content_parts,omitempty"`
	ThinkingMode      string                `json:"thinking_mode,omitempty"`
	Source            string                `json:"source,omitempty"`
	SessionID         string                `json:"session_id,omitempty"`
	Status            string                `json:"status"`
	Output            string                `json:"output,omitempty"`
	ArtifactPath      string                `json:"artifact_path,omitempty"`
	DurationMS        int64                 `json:"duration_ms,omitempty"`
	ToolCalls         int                   `json:"tool_calls,omitempty"`
	Provider          string                `json:"provider,omitempty"`
	Model             string                `json:"model,omitempty"`
	Trace             map[string]any        `json:"trace,omitempty"`
	DecompositionPlan map[string]any        `json:"decomposition_plan,omitempty"`
	Error             string                `json:"error,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

func cloneJSONMap(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		cloned := make(map[string]any, len(raw))
		for key, value := range raw {
			cloned[key] = value
		}
		return cloned
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		cloned := make(map[string]any, len(raw))
		for key, value := range raw {
			cloned[key] = value
		}
		return cloned
	}
	return decoded
}

func extractDecompositionPlanFromTrace(trace map[string]any) map[string]any {
	if len(trace) == 0 {
		return nil
	}
	raw, ok := trace["decomposition_plan"]
	if !ok {
		return nil
	}
	plan, ok := raw.(map[string]any)
	if !ok || len(plan) == 0 {
		return nil
	}
	return cloneJSONMap(plan)
}

func normalizeRunDerivedFields(run *Run) {
	if run == nil {
		return
	}
	if plan := extractDecompositionPlanFromTrace(run.Trace); plan != nil {
		run.DecompositionPlan = plan
		return
	}
	run.DecompositionPlan = cloneJSONMap(run.DecompositionPlan)
}

type RunStore interface {
	Create(ctx context.Context, run Run) (Run, error)
	Get(ctx context.Context, id string) (Run, error)
	Update(ctx context.Context, run Run) error
	List(ctx context.Context) ([]Run, error)
}

type InMemoryRunStore struct {
	mu   sync.RWMutex
	runs map[string]Run
}

func NewInMemoryRunStore() *InMemoryRunStore {
	return &InMemoryRunStore{runs: make(map[string]Run)}
}

func (s *InMemoryRunStore) Create(_ context.Context, run Run) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeRunDerivedFields(&run)
	s.runs[run.ID] = run
	return run, nil
}

func (s *InMemoryRunStore) Get(_ context.Context, id string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	normalizeRunDerivedFields(&run)
	return run, nil
}

func (s *InMemoryRunStore) Update(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return ErrRunNotFound
	}
	normalizeRunDerivedFields(&run)
	s.runs[run.ID] = run
	return nil
}

func (s *InMemoryRunStore) List(_ context.Context) ([]Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		normalizeRunDerivedFields(&run)
		runs = append(runs, run)
	}
	return runs, nil
}
