package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"openclawssy/internal/fsutil"
	"sync"
	"time"
)

type Job struct {
	ID        string `json:"id"`
	Schedule  string `json:"schedule"`
	AgentID   string `json:"agentID"`
	Message   string `json:"message"`
	Channel   string `json:"channel,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	RoomID    string `json:"room_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Enabled   bool   `json:"enabled"`
	LastRun   string `json:"lastRun"`
}

type jobUpdate struct {
	JobID   string
	RunAt   time.Time
	Disable bool
}

type RunFunc func(agentID string, message string)
type RunJobFunc func(job Job)

var ErrJobNotFound = errors.New("scheduler: job not found")

type Store struct {
	path string

	mu       sync.Mutex
	jobs     map[string]Job
	paused   bool
	lastMod  time.Time
	lastSize int64
}

type persistedJobs struct {
	Paused bool  `json:"paused,omitempty"`
	Jobs   []Job `json:"jobs"`
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("scheduler: path is required")
	}
	s := &Store{path: path, jobs: make(map[string]Job)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Add(job Job) error {
	if job.ID == "" {
		return errors.New("scheduler: job id is required")
	}
	if job.Schedule == "" {
		return errors.New("scheduler: job schedule is required")
	}
	if _, _, err := nextDue(job, time.Now().UTC()); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	s.jobs[job.ID] = job
	return s.saveLocked()
}

func (s *Store) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()

	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})
	return jobs
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	if _, ok := s.jobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(s.jobs, id)
	return s.saveLocked()
}

func (s *Store) load() error {
	return s.reloadLocked()
}

func (s *Store) reloadLocked() error {
	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.jobs = make(map[string]Job)
			s.paused = false
			s.lastMod = time.Time{}
			s.lastSize = 0
			return nil
		}
		return fmt.Errorf("scheduler: stat store: %w", err)
	}

	if info.ModTime().Equal(s.lastMod) && info.Size() == s.lastSize {
		return nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("scheduler: read store: %w", err)
	}

	var p persistedJobs
	if len(data) == 0 {
		s.jobs = make(map[string]Job)
		s.paused = false
		s.lastMod = info.ModTime()
		s.lastSize = info.Size()
		return nil
	}

	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("scheduler: parse store: %w", err)
	}

	jobs := make(map[string]Job)
	for _, job := range p.Jobs {
		jobs[job.ID] = job
	}

	s.jobs = jobs
	s.paused = p.Paused
	s.lastMod = info.ModTime()
	s.lastSize = info.Size()
	return nil
}

func (s *Store) saveLocked() error {
	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	body, err := json.MarshalIndent(persistedJobs{Paused: s.paused, Jobs: jobs}, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler: encode store: %w", err)
	}
	body = append(body, '\n')

	if err := fsutil.WriteFileAtomic(s.path, body, 0o600); err != nil {
		return err
	}

	if info, err := os.Stat(s.path); err == nil {
		s.lastMod = info.ModTime()
		s.lastSize = info.Size()
	}
	return nil
}

func (s *Store) updateAfterRun(job Job, runAt time.Time, disable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	cur, ok := s.jobs[job.ID]
	if !ok {
		return ErrJobNotFound
	}
	cur.LastRun = runAt.UTC().Format(time.RFC3339)
	if disable {
		cur.Enabled = false
	}
	s.jobs[job.ID] = cur
	return s.saveLocked()
}

func (s *Store) batchUpdateAfterRun(updates []jobUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	dirty := false
	for _, u := range updates {
		cur, ok := s.jobs[u.JobID]
		if !ok {
			continue
		}
		cur.LastRun = u.RunAt.UTC().Format(time.RFC3339)
		if u.Disable {
			cur.Enabled = false
		}
		s.jobs[u.JobID] = cur
		dirty = true
	}
	if dirty {
		return s.saveLocked()
	}
	return nil
}

func (s *Store) SetPaused(paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	s.paused = paused
	return s.saveLocked()
}

func (s *Store) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()
	return s.paused
}

func (s *Store) SetJobEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	job, ok := s.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	job.Enabled = enabled
	s.jobs[id] = job
	return s.saveLocked()
}

type Executor struct {
	store  *Store
	ticker *time.Ticker
	stopCh chan struct{}
	doneCh chan struct{}

	runJobFunc    RunJobFunc
	nowFn         func() time.Time
	maxConcurrent int
	catchUp       bool
	firstCheck    bool
}

func NewExecutor(store *Store, tickInterval time.Duration, runFn RunFunc) *Executor {
	return NewExecutorWithConcurrency(store, tickInterval, 1, runFn)
}

func NewExecutorWithConcurrency(store *Store, tickInterval time.Duration, maxConcurrent int, runFn RunFunc) *Executor {
	return NewExecutorWithPolicy(store, tickInterval, maxConcurrent, true, runFn)
}

func NewExecutorWithPolicy(store *Store, tickInterval time.Duration, maxConcurrent int, catchUp bool, runFn RunFunc) *Executor {
	if runFn == nil {
		runFn = func(string, string) {}
	}
	return NewExecutorWithJobPolicy(store, tickInterval, maxConcurrent, catchUp, func(job Job) {
		runFn(job.AgentID, job.Message)
	})
}

func NewExecutorWithJobPolicy(store *Store, tickInterval time.Duration, maxConcurrent int, catchUp bool, runFn RunJobFunc) *Executor {
	if tickInterval <= 0 {
		tickInterval = time.Second
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	if runFn == nil {
		runFn = func(Job) {}
	}
	return &Executor{
		store:         store,
		runJobFunc:    runFn,
		nowFn:         time.Now,
		maxConcurrent: maxConcurrent,
		catchUp:       catchUp,
		firstCheck:    true,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		ticker:        time.NewTicker(tickInterval),
	}
}

func (e *Executor) Start() {
	go func() {
		defer close(e.doneCh)
		for {
			select {
			case <-e.stopCh:
				return
			case <-e.ticker.C:
				e.check(e.nowFn().UTC())
			}
		}
	}()
}

func (e *Executor) Stop() {
	close(e.stopCh)
	e.ticker.Stop()
	<-e.doneCh
}

func (e *Executor) check(now time.Time) {
	if e.store == nil || e.store.IsPaused() {
		return
	}
	isFirstCheck := e.firstCheck
	e.firstCheck = false
	jobs := e.store.List()
	type dueJob struct {
		job             Job
		disableAfterRun bool
	}
	dueJobs := make([]dueJob, 0, len(jobs))
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		due, disableAfterRun, err := nextDue(job, now)
		if err != nil || !due {
			continue
		}
		if isFirstCheck && !e.catchUp && isMissedRun(job, now) {
			_ = e.store.updateAfterRun(job, now, disableAfterRun)
			continue
		}
		dueJobs = append(dueJobs, dueJob{job: job, disableAfterRun: disableAfterRun})
	}
	if len(dueJobs) == 0 {
		return
	}
	workers := e.maxConcurrent
	if workers <= 0 {
		workers = 1
	}
	if workers > len(dueJobs) {
		workers = len(dueJobs)
	}

	jobsCh := make(chan dueJob)
	updatesCh := make(chan jobUpdate, len(dueJobs))
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for item := range jobsCh {
				e.runJobFunc(item.job)
				updatesCh <- jobUpdate{JobID: item.job.ID, RunAt: now, Disable: item.disableAfterRun}
			}
		}()
	}
	for _, item := range dueJobs {
		jobsCh <- item
	}
	close(jobsCh)
	wg.Wait()
	close(updatesCh)

	var updates []jobUpdate
	for update := range updatesCh {
		updates = append(updates, update)
	}
	if len(updates) > 0 {
		_ = e.store.batchUpdateAfterRun(updates)
	}
}

func isMissedRun(job Job, now time.Time) bool {
	if strings.HasPrefix(job.Schedule, "@every ") {
		raw := strings.TrimSpace(strings.TrimPrefix(job.Schedule, "@every "))
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return false
		}
		last, err := parseLastRun(job.LastRun)
		if err != nil || last.IsZero() {
			return false
		}
		return now.Sub(last) >= d
	}
	oneShotAt, err := time.Parse(time.RFC3339, job.Schedule)
	if err != nil {
		return false
	}
	last, err := parseLastRun(job.LastRun)
	if err != nil || !last.IsZero() {
		return false
	}
	return now.After(oneShotAt)
}

func nextDue(job Job, now time.Time) (bool, bool, error) {
	if strings.HasPrefix(job.Schedule, "@every ") {
		raw := strings.TrimSpace(strings.TrimPrefix(job.Schedule, "@every "))
		d, err := time.ParseDuration(raw)
		if err != nil {
			return false, false, fmt.Errorf("scheduler: invalid duration %q: %w", raw, err)
		}
		if d <= 0 {
			return false, false, errors.New("scheduler: duration must be > 0")
		}
		last, err := parseLastRun(job.LastRun)
		if err != nil {
			return false, false, err
		}
		if last.IsZero() {
			return true, false, nil
		}
		return now.Sub(last) >= d, false, nil
	}

	oneShotAt, err := time.Parse(time.RFC3339, job.Schedule)
	if err != nil {
		return false, false, fmt.Errorf("scheduler: invalid schedule %q", job.Schedule)
	}
	last, err := parseLastRun(job.LastRun)
	if err != nil {
		return false, false, err
	}
	if !last.IsZero() {
		return false, true, nil
	}
	if now.Before(oneShotAt) {
		return false, false, nil
	}
	return true, true, nil
}

func parseLastRun(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("scheduler: invalid lastRun %q", raw)
	}
	return t, nil
}
