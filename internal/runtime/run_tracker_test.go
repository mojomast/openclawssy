package runtime

import (
	"context"
	"testing"
	"time"
)

func TestRunTracker_TrackAndCancel(t *testing.T) {
	rt := NewRunTracker()

	// Create a cancellable context
	ctx, cancel := createTestContext()
	defer cancel()

	// Track the run
	rt.Track("run-1", cancel)

	// Verify it's tracked
	if !rt.IsTracked("run-1") {
		t.Fatal("expected run-1 to be tracked")
	}

	// Cancel the run
	if err := rt.Cancel("run-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected context to be cancelled")
	}
}

func TestRunTracker_Cancel_NotFound(t *testing.T) {
	rt := NewRunTracker()

	err := rt.Cancel("nonexistent-run")
	if err != ErrRunNotFound {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestRunTracker_TrackCompositeCancelAndRemove(t *testing.T) {
	rt := NewRunTracker()
	ctx, cancel := createTestContext()
	defer cancel()

	rt.TrackComposite("instance-a", "agent-1", "run-1", cancel)
	if !rt.IsTrackedComposite("instance-a", "agent-1", "run-1") {
		t.Fatal("expected composite run to be tracked")
	}

	if err := rt.CancelComposite("instance-a", "agent-1", "run-1"); err != nil {
		t.Fatalf("unexpected composite cancel error: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected composite context to be cancelled")
	}

	rt.RemoveComposite("instance-a", "agent-1", "run-1")
	if rt.IsTrackedComposite("instance-a", "agent-1", "run-1") {
		t.Fatal("expected composite run to be removed")
	}
}

func TestRunTracker_CancelComposite_NotFound(t *testing.T) {
	rt := NewRunTracker()
	if err := rt.CancelComposite("instance-a", "agent-1", "run-missing"); err != ErrRunNotFound {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestRunTracker_Remove(t *testing.T) {
	rt := NewRunTracker()
	_, cancel := createTestContext()
	defer cancel()

	// Track then remove
	rt.Track("run-1", cancel)
	rt.Remove("run-1")

	// Verify it's no longer tracked
	if rt.IsTracked("run-1") {
		t.Fatal("expected run-1 to not be tracked after removal")
	}

	// Cancel should return not found
	err := rt.Cancel("run-1")
	if err != ErrRunNotFound {
		t.Fatalf("expected ErrRunNotFound after removal, got %v", err)
	}
}

func TestRunTracker_Count(t *testing.T) {
	rt := NewRunTracker()
	_, cancel1 := createTestContext()
	defer cancel1()
	_, cancel2 := createTestContext()
	defer cancel2()

	if rt.Count() != 0 {
		t.Fatalf("expected count 0, got %d", rt.Count())
	}

	rt.Track("run-1", cancel1)
	if rt.Count() != 1 {
		t.Fatalf("expected count 1, got %d", rt.Count())
	}

	rt.Track("run-2", cancel2)
	if rt.Count() != 2 {
		t.Fatalf("expected count 2, got %d", rt.Count())
	}

	rt.Remove("run-1")
	if rt.Count() != 1 {
		t.Fatalf("expected count 1 after removal, got %d", rt.Count())
	}
}

func TestRunTracker_NilSafety(t *testing.T) {
	var rt *RunTracker

	// These should not panic
	_, cancel := createTestContext()
	defer cancel()

	rt.Track("run-1", cancel)
	err := rt.Cancel("run-1")
	if err != ErrRunNotFound {
		t.Fatalf("expected ErrRunNotFound for nil tracker, got %v", err)
	}
	rt.Remove("run-1")
	if rt.IsTracked("run-1") {
		t.Fatal("expected false for nil tracker")
	}
	if rt.Count() != 0 {
		t.Fatalf("expected count 0 for nil tracker, got %d", rt.Count())
	}
}

func TestRunTracker_ConcurrentAccess(t *testing.T) {
	rt := NewRunTracker()

	// Test concurrent track/cancel/remove operations
	done := make(chan bool, 3)

	go func() {
		for i := 0; i < 100; i++ {
			_, cancel := createTestContext()
			rt.Track(string(rune('a'+i%26)), cancel)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			rt.Cancel(string(rune('a' + i%26)))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			rt.Remove(string(rune('a' + i%26)))
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify tracker is still functional
	_, cancel := createTestContext()
	defer cancel()
	rt.Track("final", cancel)
	if !rt.IsTracked("final") {
		t.Fatal("tracker should still work after concurrent access")
	}
}

func createTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
