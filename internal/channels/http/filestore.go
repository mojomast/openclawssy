package httpchannel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"openclawssy/internal/fsutil"
)

const defaultMaxPersistedRuns = 2000

var fileRunStoreMaxPersistedRuns = defaultMaxPersistedRuns

type FileRunStore struct {
	path string

	mu   sync.RWMutex
	runs map[string]Run
}

type persistedRuns struct {
	Runs []Run `json:"runs"`
}

func NewFileRunStore(path string) (*FileRunStore, error) {
	if path == "" {
		return nil, fmt.Errorf("runs file path is required")
	}
	s := &FileRunStore{path: path, runs: make(map[string]Run)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileRunStore) Create(_ context.Context, run Run) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = run
	return run, s.saveLocked()
}

func (s *FileRunStore) Get(_ context.Context, id string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return run, nil
}

func (s *FileRunStore) Update(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return ErrRunNotFound
	}
	s.runs[run.ID] = run
	return s.saveLocked()
}

func (s *FileRunStore) List(_ context.Context) ([]Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	return runs, nil
}

func (s *FileRunStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read runs store: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var p persistedRuns
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("parse runs store: %w", err)
	}
	for _, run := range p.Runs {
		s.runs[run.ID] = run
	}
	return nil
}

func (s *FileRunStore) saveLocked() error {
	s.compactLocked()

	runs := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.Before(runs[j].CreatedAt) })

	data, err := json.MarshalIndent(persistedRuns{Runs: runs}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runs store: %w", err)
	}
	data = append(data, '\n')
	return fsutil.WriteFileAtomic(s.path, data, 0o600)
}

func (s *FileRunStore) compactLocked() {
	maxRuns := fileRunStoreMaxPersistedRuns
	if maxRuns <= 0 || len(s.runs) <= maxRuns {
		return
	}

	terminal := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		switch strings.ToLower(strings.TrimSpace(run.Status)) {
		case "completed", "failed", "cancelled":
			terminal = append(terminal, run)
		}
	}
	if len(terminal) == 0 {
		return
	}

	toRemove := len(s.runs) - maxRuns
	if toRemove <= 0 {
		return
	}

	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].UpdatedAt.Equal(terminal[j].UpdatedAt) {
			return terminal[i].CreatedAt.Before(terminal[j].CreatedAt)
		}
		return terminal[i].UpdatedAt.Before(terminal[j].UpdatedAt)
	})

	for _, run := range terminal {
		if toRemove <= 0 {
			break
		}
		delete(s.runs, run.ID)
		toRemove--
	}
}
