package httpchannel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type ExecutionResult struct {
	Output       string
	ArtifactPath string
	DurationMS   int64
	ToolCalls    int
	Provider     string
	Model        string
	Trace        map[string]any
}

var defaultQueuedRunTracker = newQueuedRunTracker()

const queuedRunMaxAttempts = 2

const defaultQueuedRunMaxInFlight = 64

var ErrQueueFull = errors.New("httpchannel: run queue is full")

type QueueRunOptions struct {
	EventBus *RunEventBus
}

func QueueRun(ctx context.Context, store RunStore, executor RunExecutor, agentID, message, source, sessionID, thinkingMode string) (Run, error) {
	return QueueRunWithOptions(ctx, store, executor, agentID, message, source, sessionID, thinkingMode, QueueRunOptions{})
}

func QueueRunWithOptions(ctx context.Context, store RunStore, executor RunExecutor, agentID, message, source, sessionID, thinkingMode string, opts QueueRunOptions) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:           newRunID(),
		AgentID:      agentID,
		Message:      message,
		ThinkingMode: strings.TrimSpace(thinkingMode),
		Source:       source,
		SessionID:    sessionID,
		Status:       "queued",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if !defaultQueuedRunTracker.tryBegin() {
		return Run{}, ErrQueueFull
	}
	created, err := store.Create(ctx, run)
	if err != nil {
		defaultQueuedRunTracker.done()
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	go executeQueuedRun(context.Background(), store, executor, created, opts)
	return created, nil
}

func executeQueuedRun(ctx context.Context, store RunStore, executor RunExecutor, run Run, opts QueueRunOptions) {
	defer defaultQueuedRunTracker.done()
	if opts.EventBus != nil {
		defer opts.EventBus.Close(run.ID)
	}

	run.Status = "running"
	run.UpdatedAt = time.Now().UTC()
	_ = store.Update(ctx, run)
	publishQueueRunEvent(opts.EventBus, run.ID, RunEventStatus, map[string]any{"status": "running"})

	input := ExecutionInput{
		AgentID:      run.AgentID,
		Message:      run.Message,
		Source:       run.Source,
		SessionID:    run.SessionID,
		ThinkingMode: run.ThinkingMode,
	}
	if opts.EventBus != nil {
		input.OnProgress = func(eventType string, data map[string]any) {
			eventKind, ok := progressEventType(eventType)
			if !ok {
				return
			}
			publishQueueRunEvent(opts.EventBus, run.ID, eventKind, cloneProgressData(data))
		}
	}

	result, err := executeWithRetry(ctx, executor, input)
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		run.Trace = result.Trace
		run.Provider = result.Provider
		run.Model = result.Model
		run.ToolCalls = result.ToolCalls
		publishQueueRunEvent(opts.EventBus, run.ID, RunEventFailed, map[string]any{
			"status":     "failed",
			"error":      run.Error,
			"provider":   run.Provider,
			"model":      run.Model,
			"tool_calls": run.ToolCalls,
		})
	} else {
		run.Status = "completed"
		output := strings.TrimSpace(result.Output)
		if output == "" {
			output = "Run completed without assistant output. Check run trace/tool activity for details."
		}
		run.Output = output
		run.ArtifactPath = result.ArtifactPath
		run.DurationMS = result.DurationMS
		run.ToolCalls = result.ToolCalls
		run.Provider = result.Provider
		run.Model = result.Model
		run.Trace = result.Trace
		publishQueueRunEvent(opts.EventBus, run.ID, RunEventCompleted, map[string]any{
			"status":        "completed",
			"output":        run.Output,
			"artifact_path": run.ArtifactPath,
			"duration_ms":   run.DurationMS,
			"tool_calls":    run.ToolCalls,
			"provider":      run.Provider,
			"model":         run.Model,
		})
	}
	run.UpdatedAt = time.Now().UTC()
	_ = store.Update(ctx, run)
}

func publishQueueRunEvent(bus *RunEventBus, runID string, eventType RunEventType, data map[string]any) {
	if bus == nil {
		return
	}
	bus.Publish(runID, RunEvent{Type: eventType, Data: data})
}

func progressEventType(eventType string) (RunEventType, bool) {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "status":
		return RunEventStatus, true
	case "tool_end":
		return RunEventToolEnd, true
	case "model_text":
		return RunEventModelText, true
	case "completed":
		return RunEventCompleted, true
	case "failed":
		return RunEventFailed, true
	default:
		return "", false
	}
}

func cloneProgressData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = value
	}
	return out
}

func executeWithRetry(ctx context.Context, executor RunExecutor, input ExecutionInput) (ExecutionResult, error) {
	var lastResult ExecutionResult
	var lastErr error
	for attempt := 1; attempt <= queuedRunMaxAttempts; attempt++ {
		result, err := executor.Execute(ctx, input)
		if err == nil {
			if attempt > 1 {
				result.Trace = withRetryMeta(result.Trace, attempt-1)
			}
			return result, nil
		}
		lastResult = result
		lastErr = err
		if !isRetryableExecutionError(err) || attempt == queuedRunMaxAttempts {
			break
		}
	}
	if lastResult.Trace != nil {
		lastResult.Trace = withRetryMeta(lastResult.Trace, 1)
	}
	return lastResult, lastErr
}

func withRetryMeta(trace map[string]any, retries int) map[string]any {
	if retries <= 0 {
		return trace
	}
	if trace == nil {
		trace = map[string]any{}
	}
	trace["queue_retry_attempts"] = retries
	return trace
}

func isRetryableExecutionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "unexpected eof")
}

func WaitForQueuedRuns(ctx context.Context) error {
	return defaultQueuedRunTracker.wait(ctx)
}

type queuedRunTracker struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	waitCh      chan struct{}
}

func newQueuedRunTracker() *queuedRunTracker {
	ch := make(chan struct{})
	close(ch)
	return &queuedRunTracker{waitCh: ch, maxInFlight: defaultQueuedRunMaxInFlight}
}

func (t *queuedRunTracker) tryBegin() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.maxInFlight > 0 && t.inFlight >= t.maxInFlight {
		return false
	}
	if t.inFlight == 0 {
		t.waitCh = make(chan struct{})
	}
	t.inFlight++
	return true
}

func (t *queuedRunTracker) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight <= 0 {
		return
	}
	t.inFlight--
	if t.inFlight == 0 {
		close(t.waitCh)
	}
}

func (t *queuedRunTracker) wait(ctx context.Context) error {
	t.mu.Lock()
	if t.inFlight == 0 {
		t.mu.Unlock()
		return nil
	}
	ch := t.waitCh
	t.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
