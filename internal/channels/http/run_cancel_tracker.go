package httpchannel

import (
	"context"
	"errors"
	"strings"
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

func trackedRunKey(instanceID, agentID, runID string) string {
	if strings.TrimSpace(instanceID) == "" {
		return strings.TrimSpace(runID)
	}
	return strings.TrimSpace(instanceID) + ":" + strings.TrimSpace(agentID) + ":" + strings.TrimSpace(runID)
}

func (t *ActiveRunTracker) Track(runID string, cancel context.CancelFunc) {
	if t == nil || cancel == nil || runID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancels[runID] = cancel
}

func (t *ActiveRunTracker) TrackComposite(instanceID, agentID, runID string, cancel context.CancelFunc) {
	if t == nil || cancel == nil {
		return
	}
	key := trackedRunKey(instanceID, agentID, runID)
	if key == "" || key == strings.TrimSpace(runID) {
		return
	}
	t.Track(key, cancel)
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

func (t *ActiveRunTracker) CancelComposite(instanceID, agentID, runID string) error {
	key := trackedRunKey(instanceID, agentID, runID)
	if key == "" {
		return ErrTrackedRunNotFound
	}
	return t.Cancel(key)
}

func (t *ActiveRunTracker) Remove(runID string) {
	if t == nil || runID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cancels, runID)
}

func (t *ActiveRunTracker) RemoveComposite(instanceID, agentID, runID string) {
	key := trackedRunKey(instanceID, agentID, runID)
	if key == "" || key == strings.TrimSpace(runID) {
		return
	}
	t.Remove(key)
}

func (t *ActiveRunTracker) IsTracked(runID string) bool {
	if t == nil || runID == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.cancels[runID]
	return ok
}

func (t *ActiveRunTracker) IsTrackedComposite(instanceID, agentID, runID string) bool {
	key := trackedRunKey(instanceID, agentID, runID)
	if key == "" {
		return false
	}
	return t.IsTracked(key)
}
