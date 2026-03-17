package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var ErrRunNotFound = errors.New("run not found")

// RunTracker manages cancellable contexts for active runs.
// It provides thread-safe tracking and cancellation of runs by ID.
type RunTracker struct {
	mu      sync.RWMutex
	cancels map[string]context.CancelFunc
}

// NewRunTracker creates a new RunTracker instance.
func NewRunTracker() *RunTracker {
	return &RunTracker{
		cancels: make(map[string]context.CancelFunc),
	}
}

// Track registers a run with its cancel function.
// This should be called before a run starts.
func (rt *RunTracker) Track(runID string, cancel context.CancelFunc) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.cancels[runID] = cancel
}

// TrackComposite registers a run using composite instance/agent/run identity.
func (rt *RunTracker) TrackComposite(instanceID, agentID, runID string, cancel context.CancelFunc) {
	rt.Track(compositeRunKey(instanceID, agentID, runID), cancel)
}

// Cancel triggers cancellation for a run by its ID.
// Returns ErrRunNotFound if the run is not tracked.
func (rt *RunTracker) Cancel(runID string) error {
	if rt == nil {
		return ErrRunNotFound
	}
	rt.mu.RLock()
	cancel, ok := rt.cancels[runID]
	rt.mu.RUnlock()

	if !ok {
		return ErrRunNotFound
	}

	cancel()
	return nil
}

// CancelComposite triggers cancellation for a run by its composite identity.
func (rt *RunTracker) CancelComposite(instanceID, agentID, runID string) error {
	return rt.Cancel(compositeRunKey(instanceID, agentID, runID))
}

// Remove unregisters a run from tracking.
// This should be called when a run completes (success or failure).
func (rt *RunTracker) Remove(runID string) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.cancels, runID)
}

// RemoveComposite unregisters a run tracked by composite identity.
func (rt *RunTracker) RemoveComposite(instanceID, agentID, runID string) {
	rt.Remove(compositeRunKey(instanceID, agentID, runID))
}

// IsTracked checks if a run ID is currently being tracked.
func (rt *RunTracker) IsTracked(runID string) bool {
	if rt == nil {
		return false
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	_, ok := rt.cancels[runID]
	return ok
}

// IsTrackedComposite checks whether a composite run key is being tracked.
func (rt *RunTracker) IsTrackedComposite(instanceID, agentID, runID string) bool {
	return rt.IsTracked(compositeRunKey(instanceID, agentID, runID))
}

// Count returns the number of currently tracked runs.
func (rt *RunTracker) Count() int {
	if rt == nil {
		return 0
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.cancels)
}

func compositeRunKey(instanceID, agentID, runID string) string {
	instanceID = strings.TrimSpace(instanceID)
	agentID = strings.TrimSpace(agentID)
	runID = strings.TrimSpace(runID)
	if instanceID == "" || agentID == "" || runID == "" {
		return ""
	}
	return instanceID + ":" + agentID + ":" + runID
}
