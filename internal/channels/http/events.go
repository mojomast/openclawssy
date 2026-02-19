package httpchannel

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RunEventType string

const (
	RunEventStatus    RunEventType = "status"
	RunEventToolEnd   RunEventType = "tool_end"
	RunEventModelText RunEventType = "model_text"
	RunEventCompleted RunEventType = "completed"
	RunEventFailed    RunEventType = "failed"
	RunEventHeartbeat RunEventType = "heartbeat"
)

type RunEvent struct {
	ID        int64          `json:"id"`
	Type      RunEventType   `json:"type"`
	RunID     string         `json:"run_id"`
	Timestamp time.Time      `json:"ts"`
	Data      map[string]any `json:"data,omitempty"`
}

const (
	defaultRunEventReplayLimit      = 128
	defaultRunEventSubscriberBuffer = 32
	defaultRunEventMaxRuns          = 2048
)

type RunEventBus struct {
	mu               sync.Mutex
	runs             map[string]*runEventState
	runOrder         []string
	replayLimit      int
	subscriberBuffer int
	maxRuns          int
}

type runEventState struct {
	nextID         int64
	nextSubID      int64
	replay         []RunEvent
	subscribers    map[int64]chan RunEvent
	terminal       bool
	terminalEvents []RunEvent
	dropped        atomic.Uint64
}

func NewRunEventBus(replayLimit int) *RunEventBus {
	if replayLimit <= 0 {
		replayLimit = defaultRunEventReplayLimit
	}
	return &RunEventBus{
		runs:             make(map[string]*runEventState),
		replayLimit:      replayLimit,
		subscriberBuffer: defaultRunEventSubscriberBuffer,
		maxRuns:          defaultRunEventMaxRuns,
	}
}

func (b *RunEventBus) Subscribe(runID string, lastEventID int64) (<-chan RunEvent, func()) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}

	b.mu.Lock()
	state := b.ensureRunLocked(runID)
	replay := make([]RunEvent, 0, len(state.replay))
	for _, event := range state.replay {
		if event.ID > lastEventID {
			replay = append(replay, event)
		}
	}
	if state.terminal && len(replay) == 0 && len(state.terminalEvents) > 0 {
		replay = append(replay, state.terminalEvents...)
	}

	capacity := b.subscriberBuffer
	if needed := len(replay) + b.subscriberBuffer; needed > capacity {
		capacity = needed
	}
	if capacity <= 0 {
		capacity = 1
	}
	ch := make(chan RunEvent, capacity)
	for _, event := range replay {
		ch <- event
	}

	if state.terminal {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}

	state.nextSubID++
	subID := state.nextSubID
	state.subscribers[subID] = ch
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.unsubscribe(runID, subID)
		})
	}
	return ch, unsubscribe
}

func (b *RunEventBus) Publish(runID string, event RunEvent) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}

	b.mu.Lock()
	state := b.ensureRunLocked(runID)

	event.RunID = runID
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.ID <= 0 {
		state.nextID++
		event.ID = state.nextID
	} else if event.ID > state.nextID {
		state.nextID = event.ID
	}

	state.replay = append(state.replay, event)
	if len(state.replay) > b.replayLimit {
		state.replay = append([]RunEvent(nil), state.replay[len(state.replay)-b.replayLimit:]...)
	}
	if isTerminalRunEventType(event.Type) {
		state.terminal = true
		state.terminalEvents = []RunEvent{event}
	}

	for _, sub := range state.subscribers {
		if !enqueueRunEvent(sub, event) {
			state.dropped.Add(1)
		}
	}
	b.mu.Unlock()
}

func (b *RunEventBus) Close(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}

	b.mu.Lock()
	state, ok := b.runs[runID]
	if !ok {
		b.mu.Unlock()
		return
	}
	state.terminal = true
	for id, sub := range state.subscribers {
		delete(state.subscribers, id)
		close(sub)
	}
	b.mu.Unlock()
}

func (b *RunEventBus) unsubscribe(runID string, subID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, ok := b.runs[runID]
	if !ok {
		return
	}
	sub, ok := state.subscribers[subID]
	if !ok {
		return
	}
	delete(state.subscribers, subID)
	close(sub)
}

func (b *RunEventBus) ensureRunLocked(runID string) *runEventState {
	state, ok := b.runs[runID]
	if ok {
		return state
	}
	state = &runEventState{subscribers: make(map[int64]chan RunEvent)}
	b.runs[runID] = state
	b.runOrder = append(b.runOrder, runID)
	b.pruneRunsLocked()
	return state
}

func (b *RunEventBus) pruneRunsLocked() {
	if b.maxRuns <= 0 {
		return
	}
	for len(b.runs) > b.maxRuns {
		removed := false
		for idx := 0; idx < len(b.runOrder); idx++ {
			runID := b.runOrder[idx]
			state, ok := b.runs[runID]
			if !ok {
				b.runOrder = append(b.runOrder[:idx], b.runOrder[idx+1:]...)
				removed = true
				break
			}
			if state.terminal && len(state.subscribers) == 0 {
				delete(b.runs, runID)
				b.runOrder = append(b.runOrder[:idx], b.runOrder[idx+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func enqueueRunEvent(ch chan RunEvent, event RunEvent) bool {
	select {
	case ch <- event:
		return true
	default:
	}

	select {
	case <-ch:
	default:
	}

	select {
	case ch <- event:
		return true
	default:
		return false
	}
}

func isTerminalRunEventType(eventType RunEventType) bool {
	switch eventType {
	case RunEventCompleted, RunEventFailed:
		return true
	default:
		return false
	}
}
