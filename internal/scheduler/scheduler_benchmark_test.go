package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkExecutorCheck(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "scheduler_benchmark")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storePath := filepath.Join(tmpDir, "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		b.Fatalf("new store: %v", err)
	}

	numJobs := 100
	for i := 0; i < numJobs; i++ {
		job := Job{
			ID:       fmt.Sprintf("job-%d", i),
			Schedule: "@every 1ms",
			AgentID:  "agent",
			Message:  "run",
			Enabled:  true,
		}
		if err := store.Add(job); err != nil {
			b.Fatalf("add job: %v", err)
		}
	}

	exec := NewExecutorWithConcurrency(store, time.Millisecond, 10, func(agentID string, message string) {
	})

	baseTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now := baseTime.Add(time.Duration(i+1) * time.Second)
		exec.check(now)
	}
}
