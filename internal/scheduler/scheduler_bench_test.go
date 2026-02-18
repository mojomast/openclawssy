package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkStore_List(b *testing.B) {
	dir, err := os.MkdirTemp("", "scheduler-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	storePath := filepath.Join(dir, "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		job := Job{
			ID:       "job-" + string(rune(i)),
			Schedule: "@every 1m",
			AgentID:  "agent",
			Message:  "hello",
			Enabled:  true,
		}
		if err := store.Add(job); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.List()
	}
}

func BenchmarkStore_IsPaused(b *testing.B) {
	dir, err := os.MkdirTemp("", "scheduler-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	storePath := filepath.Join(dir, "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		job := Job{
			ID:       "job-" + string(rune(i)),
			Schedule: "@every 1m",
			AgentID:  "agent",
			Message:  "hello",
			Enabled:  true,
		}
		if err := store.Add(job); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.IsPaused()
	}
}

func BenchmarkExecutor_Check(b *testing.B) {
	dir, err := os.MkdirTemp("", "scheduler-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	storePath := filepath.Join(dir, "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		job := Job{
			ID:       "job-" + string(rune(i)),
			Schedule: "@every 1h", // Make sure they are not due
			AgentID:  "agent",
			Message:  "hello",
			Enabled:  true,
		}
		if err := store.Add(job); err != nil {
			b.Fatal(err)
		}
	}

	exec := NewExecutor(store, time.Second, nil)
	now := time.Now().UTC()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec.check(now)
	}
}
