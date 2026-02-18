package httpchannel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type traceExecutor struct {
	result ExecutionResult
	err    error
}

type blockingExecutor struct {
	release <-chan struct{}
}

type flakyRetryableExecutor struct {
	mu    sync.Mutex
	calls int
}

type progressPublishingExecutor struct{}

func (f *flakyRetryableExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		return ExecutionResult{Trace: map[string]any{"first_attempt": true}}, context.DeadlineExceeded
	}
	return ExecutionResult{Output: "ok", Trace: map[string]any{"attempt": f.calls}}, nil
}

func (t traceExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	return t.result, t.err
}

func (b blockingExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	<-b.release
	return ExecutionResult{Output: "done"}, nil
}

func (p progressPublishingExecutor) Execute(_ context.Context, input ExecutionInput) (ExecutionResult, error) {
	if input.OnProgress != nil {
		input.OnProgress("tool_end", map[string]any{"tool": "fs.list", "summary": "listed files"})
		input.OnProgress("model_text", map[string]any{"text": "partial", "partial": true})
	}
	return ExecutionResult{Output: "done"}, nil
}

func TestQueueRunPersistsSessionAndTrace(t *testing.T) {
	store := NewInMemoryRunStore()
	queued, err := QueueRun(context.Background(), store, traceExecutor{result: ExecutionResult{Output: "ok", Trace: map[string]any{"run_id": "trace-run", "prompt_length": float64(12)}}}, "agent-1", "hello", "dashboard", "chat_123", "always")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			if run.ThinkingMode != "always" {
				t.Fatalf("expected thinking mode to persist, got %q", run.ThinkingMode)
			}
			if run.SessionID != "chat_123" {
				t.Fatalf("expected session_id chat_123, got %q", run.SessionID)
			}
			if run.Trace == nil || run.Trace["run_id"] != "trace-run" {
				t.Fatalf("expected trace to persist, got %#v", run.Trace)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQueueRunPersistsTraceOnFailure(t *testing.T) {
	store := NewInMemoryRunStore()
	queued, err := QueueRun(context.Background(), store, traceExecutor{result: ExecutionResult{Trace: map[string]any{"run_id": "trace-fail"}}, err: context.DeadlineExceeded}, "agent-1", "hello", "dashboard", "chat_123", "")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "failed" {
			if run.Trace == nil || run.Trace["run_id"] != "trace-fail" {
				t.Fatalf("expected failure trace to persist, got %#v", run.Trace)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not fail in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWaitForQueuedRunsBlocksUntilCompletion(t *testing.T) {
	store := NewInMemoryRunStore()
	release := make(chan struct{})
	_, err := QueueRun(context.Background(), store, blockingExecutor{release: release}, "agent-1", "hello", "dashboard", "chat_123", "")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shortCancel()
	err = WaitForQueuedRuns(shortCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}

	close(release)
	longCtx, longCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer longCancel()
	if err := WaitForQueuedRuns(longCtx); err != nil {
		t.Fatalf("expected in-flight drain success, got %v", err)
	}
}

func TestQueueRunRetriesRetryableExecutorFailureOnce(t *testing.T) {
	store := NewInMemoryRunStore()
	exec := &flakyRetryableExecutor{}
	queued, err := QueueRun(context.Background(), store, exec, "agent-1", "hello", "dashboard", "chat_123", "")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			if run.Output != "ok" {
				t.Fatalf("expected successful retry output, got %q", run.Output)
			}
			if run.Trace == nil || run.Trace["queue_retry_attempts"] != 1 {
				t.Fatalf("expected retry metadata in trace, got %#v", run.Trace)
			}
			if exec.calls != 2 {
				t.Fatalf("expected exactly two executor attempts, got %d", exec.calls)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQueueRunWritesFallbackOutputWhenExecutorReturnsEmptyText(t *testing.T) {
	store := NewInMemoryRunStore()
	queued, err := QueueRun(context.Background(), store, traceExecutor{result: ExecutionResult{Output: "   ", Trace: map[string]any{"run_id": "trace-empty"}}}, "agent-1", "hello", "dashboard", "chat_123", "")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			if run.Output == "" || run.Output == "(completed with no output)" {
				t.Fatalf("expected explicit non-empty fallback output, got %q", run.Output)
			}
			if run.Trace == nil || run.Trace["run_id"] != "trace-empty" {
				t.Fatalf("expected trace to persist, got %#v", run.Trace)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQueueRunRejectsWhenQueueLimitReached(t *testing.T) {
	store := NewInMemoryRunStore()

	defaultQueuedRunTracker.mu.Lock()
	originalLimit := defaultQueuedRunTracker.maxInFlight
	defaultQueuedRunTracker.maxInFlight = 1
	defaultQueuedRunTracker.mu.Unlock()
	defer func() {
		defaultQueuedRunTracker.mu.Lock()
		defaultQueuedRunTracker.maxInFlight = originalLimit
		defaultQueuedRunTracker.mu.Unlock()
	}()

	release := make(chan struct{})
	_, err := QueueRun(context.Background(), store, blockingExecutor{release: release}, "agent-1", "hello", "dashboard", "chat_123", "")
	if err != nil {
		t.Fatalf("queue first run: %v", err)
	}

	_, err = QueueRun(context.Background(), store, blockingExecutor{release: release}, "agent-1", "second", "dashboard", "chat_123", "")
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForQueuedRuns(ctx); err != nil {
		t.Fatalf("wait for queued runs: %v", err)
	}
}

func TestQueueRunWithOptionsPublishesStatusAndTerminalEvents(t *testing.T) {
	store := NewInMemoryRunStore()
	eventBus := NewRunEventBus(16)

	queued, err := QueueRunWithOptions(
		context.Background(),
		store,
		traceExecutor{result: ExecutionResult{Output: "ok"}},
		"agent-1",
		"hello",
		"dashboard",
		"chat_123",
		"",
		QueueRunOptions{EventBus: eventBus},
	)
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ch, unsubscribe := eventBus.Subscribe(queued.ID, 0)
	defer unsubscribe()
	var events []RunEvent
	for event := range ch {
		events = append(events, event)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least status + completed events, got %d", len(events))
	}
	if events[0].Type != RunEventStatus {
		t.Fatalf("expected first event status, got %q", events[0].Type)
	}
	last := events[len(events)-1]
	if last.Type != RunEventCompleted {
		t.Fatalf("expected terminal completed event, got %q", last.Type)
	}
	if output := last.Data["output"]; output != "ok" {
		t.Fatalf("expected completed output metadata, got %#v", last.Data)
	}
}

func TestQueueRunWithOptionsPublishesProgressEvents(t *testing.T) {
	store := NewInMemoryRunStore()
	eventBus := NewRunEventBus(32)

	queued, err := QueueRunWithOptions(
		context.Background(),
		store,
		progressPublishingExecutor{},
		"agent-1",
		"hello",
		"dashboard",
		"chat_123",
		"",
		QueueRunOptions{EventBus: eventBus},
	)
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ch, unsubscribe := eventBus.Subscribe(queued.ID, 0)
	defer unsubscribe()
	seenTool := false
	seenText := false
	seenCompleted := false
	for event := range ch {
		switch event.Type {
		case RunEventToolEnd:
			seenTool = true
		case RunEventModelText:
			seenText = true
		case RunEventCompleted:
			seenCompleted = true
		}
	}
	if !seenTool {
		t.Fatal("expected tool_end progress event")
	}
	if !seenText {
		t.Fatal("expected model_text progress event")
	}
	if !seenCompleted {
		t.Fatal("expected completed terminal event")
	}
}

func TestQueueRunWithOptionsPublishesFailedTerminalEvent(t *testing.T) {
	store := NewInMemoryRunStore()
	eventBus := NewRunEventBus(16)

	queued, err := QueueRunWithOptions(
		context.Background(),
		store,
		traceExecutor{result: ExecutionResult{Trace: map[string]any{"attempt": 1}}, err: context.DeadlineExceeded},
		"agent-1",
		"hello",
		"dashboard",
		"chat_123",
		"",
		QueueRunOptions{EventBus: eventBus},
	)
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ch, unsubscribe := eventBus.Subscribe(queued.ID, 0)
	defer unsubscribe()
	seenFailed := false
	for event := range ch {
		if event.Type == RunEventFailed {
			seenFailed = true
			if event.Data["error"] == "" {
				t.Fatalf("expected failed event to include error payload, got %#v", event.Data)
			}
		}
	}
	if !seenFailed {
		t.Fatal("expected failed terminal event")
	}
}
