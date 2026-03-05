package httpchannel

import (
	"context"
	"errors"
	"sync"
)

var ErrTrackedRunNotFound = errors.New("tracked run not found")

type ActiveRunTracker struct {
	mu      sync.RWMutex
	cancels map[string]context.CancelFunc
}

func NewActiveRunTracker() *ActiveRunTracker {
	return &ActiveRunTracker{cancels: make(map[string]context.CancelFunc)}
}

func (t *ActiveRunTracker) Track(runID string, cancel context.CancelFunc) {
	if t == nil || cancel == nil || runID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancels[runID] = cancel
}

func (t *ActiveRunTracker) Cancel(runID string) error {
	if t == nil {
		return ErrTrackedRunNotFound
	}
	t.mu.RLock()
	cancel, ok := t.cancels[runID]
	t.mu.RUnlock()
	if !ok {
		return ErrTrackedRunNotFound
	}
	cancel()
	return nil
}

func (t *ActiveRunTracker) Remove(runID string) {
	if t == nil || runID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cancels, runID)
}
