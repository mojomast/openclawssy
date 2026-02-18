package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreAddListRemove(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:       "job-1",
		Schedule: "@every 1m",
		AgentID:  "agent-alpha",
		Message:  "hello",
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != job.ID {
		t.Fatalf("expected job id %q, got %q", job.ID, jobs[0].ID)
	}

	if err := store.Remove(job.ID); err != nil {
		t.Fatalf("remove job: %v", err)
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("expected 0 jobs after remove, got %d", got)
	}
}

func TestStorePersistenceReload(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:       "job-persist",
		Schedule: "@every 10s",
		AgentID:  "agent-beta",
		Message:  "persist me",
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	reloaded, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}

	jobs := reloaded.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 reloaded job, got %d", len(jobs))
	}
	if jobs[0].ID != job.ID || jobs[0].AgentID != job.AgentID || jobs[0].Message != job.Message {
		t.Fatalf("reloaded job mismatch: %+v", jobs[0])
	}
}

func TestStoreUpdateAfterRunPreservesJobsAddedByOtherStoreInstance(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	storeA, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store A: %v", err)
	}
	storeB, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store B: %v", err)
	}

	job1 := Job{ID: "job-1", Schedule: "@every 1m", AgentID: "agent", Message: "one", Enabled: true}
	if err := storeA.Add(job1); err != nil {
		t.Fatalf("add job-1: %v", err)
	}
	job2 := Job{ID: "job-2", Schedule: "@every 1m", AgentID: "agent", Message: "two", Enabled: true}
	if err := storeB.Add(job2); err != nil {
		t.Fatalf("add job-2 from separate store: %v", err)
	}

	if err := storeA.updateAfterRun(job1, time.Now().UTC(), false); err != nil {
		t.Fatalf("updateAfterRun: %v", err)
	}

	reloaded, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	jobs := reloaded.List()
	if len(jobs) != 2 {
		t.Fatalf("expected both jobs after cross-instance update, got %#v", jobs)
	}
}

func TestExecutorTriggerExecution(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:       "job-run",
		Schedule: "@every 1ms",
		AgentID:  "agent-gamma",
		Message:  "run now",
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	var mu sync.Mutex
	runs := 0
	var gotAgent string
	var gotMessage string

	exec := NewExecutor(store, time.Millisecond, func(agentID string, message string) {
		mu.Lock()
		defer mu.Unlock()
		runs++
		gotAgent = agentID
		gotMessage = message
	})

	now := time.Now().UTC()
	exec.check(now)

	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Fatalf("expected 1 run trigger, got %d", runs)
	}
	if gotAgent != job.AgentID || gotMessage != job.Message {
		t.Fatalf("unexpected run args: agent=%q message=%q", gotAgent, gotMessage)
	}

	updated := store.List()[0]
	if updated.LastRun == "" {
		t.Fatal("expected lastRun to be updated")
	}
}

func TestExecutorRunJobCallbackReceivesDeliveryMetadata(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:        "job-delivery",
		Schedule:  "@every 1ms",
		AgentID:   "agent-gamma",
		Message:   "run now",
		Channel:   "dashboard",
		UserID:    "dashboard_user",
		RoomID:    "dashboard",
		SessionID: "chat_123",
		Enabled:   true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	var got Job
	exec := NewExecutorWithJobPolicy(store, time.Millisecond, 1, true, func(input Job) {
		got = input
	})
	exec.check(time.Now().UTC())

	if got.ID != job.ID || got.Channel != job.Channel || got.UserID != job.UserID || got.RoomID != job.RoomID || got.SessionID != job.SessionID {
		t.Fatalf("expected delivery metadata to propagate, got %+v", got)
	}
}

func TestExecutorCheckRunsDueJobsConcurrently(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := store.Add(Job{ID: "job-concurrent-" + time.Now().Add(time.Duration(i)).Format("150405.000000000"), Schedule: "@every 1ms", AgentID: "agent", Message: "run", Enabled: true}); err != nil {
			t.Fatalf("add job %d: %v", i, err)
		}
	}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	exec := NewExecutorWithConcurrency(store, time.Millisecond, 2, func(agentID string, message string) {
		_ = agentID
		_ = message
		started <- struct{}{}
		<-release
	})

	done := make(chan struct{})
	go func() {
		exec.check(time.Now().UTC())
		close(done)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for concurrent worker %d", i+1)
		}
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("executor check did not finish")
	}

	for _, job := range store.List() {
		if job.LastRun == "" {
			t.Fatalf("expected lastRun for job %q", job.ID)
		}
	}
}

func TestExecutorCheckHonorsConcurrencyLimit(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := store.Add(Job{ID: "job-limit-" + time.Now().Add(time.Duration(i)).Format("150405.000000000"), Schedule: "@every 1ms", AgentID: "agent", Message: "run", Enabled: true}); err != nil {
			t.Fatalf("add job %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	current := 0
	maxConcurrent := 0
	exec := NewExecutorWithConcurrency(store, time.Millisecond, 2, func(agentID string, message string) {
		_ = agentID
		_ = message
		mu.Lock()
		current++
		if current > maxConcurrent {
			maxConcurrent = current
		}
		mu.Unlock()

		time.Sleep(40 * time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()
	})

	exec.check(time.Now().UTC())
	if maxConcurrent > 2 {
		t.Fatalf("expected max concurrent runs <= 2, got %d", maxConcurrent)
	}
	if maxConcurrent < 2 {
		t.Fatalf("expected worker pool to run at least two jobs concurrently, got %d", maxConcurrent)
	}
}

func TestExecutorRestartResumesEveryWithoutReplay(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	job := Job{ID: "job-restart-every", Schedule: "@every 1s", AgentID: "agent", Message: "run", Enabled: true}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	runs := 0
	exec := NewExecutor(store, time.Second, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	firstRunAt := time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
	exec.check(firstRunAt)
	if runs != 1 {
		t.Fatalf("expected first execution, got %d", runs)
	}

	reloaded, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	reloadedExec := NewExecutor(reloaded, time.Second, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	restartAt := firstRunAt.Add(10 * time.Second)
	reloadedExec.check(restartAt)
	reloadedExec.check(restartAt.Add(200 * time.Millisecond))

	if runs != 2 {
		t.Fatalf("expected single post-restart catch-up run with no replay, got %d", runs)
	}
}

func TestExecutorRestartRunsMissedOneShotOnceAndDisables(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	scheduledAt := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	job := Job{ID: "job-one-shot", Schedule: scheduledAt.Format(time.RFC3339), AgentID: "agent", Message: "run once", Enabled: true}
	if err := store.Add(job); err != nil {
		t.Fatalf("add one-shot job: %v", err)
	}

	runs := 0
	reloaded, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	reloadedExec := NewExecutor(reloaded, time.Second, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	now := scheduledAt.Add(2 * time.Hour)
	reloadedExec.check(now)

	if runs != 1 {
		t.Fatalf("expected missed one-shot to run once, got %d", runs)
	}

	reloadedAgain, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store again: %v", err)
	}
	jobs := reloadedAgain.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one job after reload, got %d", len(jobs))
	}
	if jobs[0].Enabled {
		t.Fatalf("expected one-shot job to be disabled after run: %+v", jobs[0])
	}
	if jobs[0].LastRun == "" {
		t.Fatalf("expected one-shot job to persist LastRun: %+v", jobs[0])
	}

	reloadedExecAgain := NewExecutor(reloadedAgain, time.Second, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	reloadedExecAgain.check(now.Add(5 * time.Hour))
	if runs != 1 {
		t.Fatalf("expected one-shot to stay disabled after restart, got %d runs", runs)
	}
}

func TestExecutorHonorsGlobalPauseAndResume(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Add(Job{ID: "job-paused", Schedule: "@every 1ms", AgentID: "agent", Message: "run", Enabled: true}); err != nil {
		t.Fatalf("add job: %v", err)
	}
	if err := store.SetPaused(true); err != nil {
		t.Fatalf("set paused: %v", err)
	}

	runs := 0
	exec := NewExecutor(store, time.Second, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	exec.check(time.Now().UTC())
	if runs != 0 {
		t.Fatalf("expected paused scheduler to suppress runs, got %d", runs)
	}

	if err := store.SetPaused(false); err != nil {
		t.Fatalf("resume scheduler: %v", err)
	}
	exec.check(time.Now().UTC())
	if runs != 1 {
		t.Fatalf("expected resumed scheduler to execute jobs, got %d", runs)
	}
}

func TestSetJobEnabledPersistsAcrossRestart(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Add(Job{ID: "job-toggle", Schedule: "@every 1m", AgentID: "agent", Message: "run", Enabled: true}); err != nil {
		t.Fatalf("add job: %v", err)
	}
	if err := store.SetJobEnabled("job-toggle", false); err != nil {
		t.Fatalf("disable job: %v", err)
	}

	reloaded, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	jobs := reloaded.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one job after reload, got %d", len(jobs))
	}
	if jobs[0].Enabled {
		t.Fatalf("expected job to remain disabled after reload: %+v", jobs[0])
	}
}

func TestExecutorCatchUpDisabledSkipsMissedRecurringRunsOnFirstCheck(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	lastRun := time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
	if err := store.Add(Job{ID: "job-catch-up", Schedule: "@every 1m", AgentID: "agent", Message: "run", Enabled: true, LastRun: lastRun.Format(time.RFC3339)}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	runs := 0
	exec := NewExecutorWithPolicy(store, time.Second, 2, false, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	now := lastRun.Add(10 * time.Minute)
	exec.check(now)
	if runs != 0 {
		t.Fatalf("expected missed run to be skipped when catch_up=false, got %d", runs)
	}

	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one job in store, got %d", len(jobs))
	}
	if jobs[0].LastRun == "" {
		t.Fatalf("expected skipped run to update LastRun timestamp")
	}

	exec.check(now.Add(2 * time.Minute))
	if runs != 1 {
		t.Fatalf("expected subsequent due run to execute after skip, got %d", runs)
	}
}

func TestExecutorCatchUpDisabledSkipsMissedOneShot(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	oneShotAt := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	if err := store.Add(Job{ID: "job-one-shot-skip", Schedule: oneShotAt.Format(time.RFC3339), AgentID: "agent", Message: "run once", Enabled: true}); err != nil {
		t.Fatalf("add one-shot: %v", err)
	}

	runs := 0
	exec := NewExecutorWithPolicy(store, time.Second, 1, false, func(agentID string, message string) {
		_ = agentID
		_ = message
		runs++
	})
	exec.check(oneShotAt.Add(2 * time.Hour))
	if runs != 0 {
		t.Fatalf("expected missed one-shot to be skipped when catch_up=false, got %d", runs)
	}
	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one job after skip, got %d", len(jobs))
	}
	if jobs[0].Enabled {
		t.Fatalf("expected skipped one-shot to be disabled, got %+v", jobs[0])
	}
}

func TestExecutorStressHandlesMultipleDueJobsWithBoundedWorkers(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	const totalJobs = 24
	for i := 0; i < totalJobs; i++ {
		id := "job-stress-" + time.Now().Add(time.Duration(i)).Format("150405.000000000")
		if err := store.Add(Job{ID: id, Schedule: "@every 1ms", AgentID: "agent", Message: "run", Enabled: true}); err != nil {
			t.Fatalf("add job %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	maxConcurrent := 0
	inFlight := 0
	runs := 0
	exec := NewExecutorWithPolicy(store, time.Second, 4, true, func(agentID string, message string) {
		_ = agentID
		_ = message
		mu.Lock()
		runs++
		inFlight++
		if inFlight > maxConcurrent {
			maxConcurrent = inFlight
		}
		mu.Unlock()
		time.Sleep(8 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
	})

	exec.check(time.Now().UTC())
	if runs != totalJobs {
		t.Fatalf("expected %d runs, got %d", totalJobs, runs)
	}
	if maxConcurrent > 4 {
		t.Fatalf("expected worker bound <= 4, got %d", maxConcurrent)
	}
	for _, job := range store.List() {
		if job.LastRun == "" {
			t.Fatalf("expected LastRun to be persisted for %q", job.ID)
		}
	}
}

func TestExecutorStartAndStop(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Add(Job{ID: "job-start-stop", Schedule: "@every 1ms", AgentID: "agent", Message: "run", Enabled: true}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	runs := make(chan struct{}, 1)
	exec := NewExecutorWithPolicy(store, time.Millisecond, 1, true, func(agentID string, message string) {
		_ = agentID
		_ = message
		select {
		case runs <- struct{}{}:
		default:
		}
	})

	exec.Start()
	select {
	case <-runs:
	case <-time.After(2 * time.Second):
		t.Fatal("expected scheduled run while executor is started")
	}
	exec.Stop()
}

func TestNewExecutorWithPolicyAppliesFallbackDefaults(t *testing.T) {
	exec := NewExecutorWithPolicy(nil, 0, 0, false, nil)
	if exec.maxConcurrent != 1 {
		t.Fatalf("expected fallback maxConcurrent=1, got %d", exec.maxConcurrent)
	}
	if exec.catchUp {
		t.Fatal("expected catchUp to preserve explicit false")
	}
	if exec.ticker == nil || exec.stopCh == nil || exec.doneCh == nil {
		t.Fatalf("expected executor channels/ticker initialized: %+v", exec)
	}
	exec.Start()
	exec.Stop()
}

func TestStoreValidationAndLoadErrors(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("expected empty store path error")
	}

	storePath := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(storePath, []byte("{not-json}"), 0o600); err != nil {
		t.Fatalf("write malformed scheduler file: %v", err)
	}
	if _, err := NewStore(storePath); err == nil {
		t.Fatal("expected parse error for malformed scheduler file")
	}
}

func TestStoreRemoveAndEnableMissingJobs(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Remove("missing"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound from remove, got %v", err)
	}
	if err := store.SetJobEnabled("missing", true); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound from set enabled, got %v", err)
	}
}

func TestNextDueAndParseLastRunValidation(t *testing.T) {
	now := time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
	if _, _, err := nextDue(Job{ID: "bad-duration", Schedule: "@every nope"}, now); err == nil {
		t.Fatal("expected invalid duration error")
	}
	if _, _, err := nextDue(Job{ID: "zero-duration", Schedule: "@every 0s"}, now); err == nil {
		t.Fatal("expected zero duration error")
	}
	if _, _, err := nextDue(Job{ID: "bad-schedule", Schedule: "daily"}, now); err == nil {
		t.Fatal("expected invalid schedule error")
	}

	due, disable, err := nextDue(Job{ID: "one-shot-future", Schedule: now.Add(time.Hour).Format(time.RFC3339)}, now)
	if err != nil || due || disable {
		t.Fatalf("expected future one-shot not due, got due=%v disable=%v err=%v", due, disable, err)
	}

	due, disable, err = nextDue(Job{ID: "one-shot-ran", Schedule: now.Add(-time.Hour).Format(time.RFC3339), LastRun: now.Add(-30 * time.Minute).Format(time.RFC3339)}, now)
	if err != nil || due || !disable {
		t.Fatalf("expected ran one-shot to be disabled, got due=%v disable=%v err=%v", due, disable, err)
	}

	if _, err := parseLastRun("not-a-time"); err == nil {
		t.Fatal("expected invalid lastRun parse error")
	}
}
