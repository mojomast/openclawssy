package promptstack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"openclawssy/internal/fsutil"
)

var (
	ErrInvalidAgentID  = errors.New("promptstack: invalid agent id")
	ErrVersionNotFound = errors.New("promptstack: version not found")
)

type VersionStore struct {
	controlPlaneDir string
	instanceID      string
	nowFn           func() time.Time
	mu              sync.Mutex
}

type storedLayerHistory struct {
	Current PromptLayer   `json:"current"`
	History []PromptLayer `json:"history"`
}

func NewVersionStore(controlPlaneDir string, instanceID ...string) (*VersionStore, error) {
	trimmed := strings.TrimSpace(controlPlaneDir)
	if trimmed == "" {
		return nil, errors.New("promptstack: control plane directory is required")
	}
	resolvedInstanceID := ""
	if len(instanceID) > 0 {
		resolvedInstanceID = strings.TrimSpace(instanceID[0])
	}
	return &VersionStore{
		controlPlaneDir: trimmed,
		instanceID:      resolvedInstanceID,
		nowFn:           time.Now,
	}, nil
}

func (s *VersionStore) GetCurrent(agentID string) (PromptStack, error) {
	normalizedAgentID, err := normalizeAgentID(agentID)
	if err != nil {
		return PromptStack{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stack := NewPromptStack()
	for _, layerID := range layerOrder {
		history, err := s.readLayerHistoryLocked(normalizedAgentID, layerID)
		if err != nil {
			return PromptStack{}, err
		}
		if history.Current.Version == 0 && history.Current.Content == "" {
			continue
		}
		if err := stack.SetLayer(history.Current); err != nil {
			return PromptStack{}, err
		}
	}

	return stack, nil
}

func (s *VersionStore) UpdateLayer(agentID, layerID, content string) (PromptLayer, error) {
	normalizedAgentID, normalizedLayerID, err := s.normalizeIDs(agentID, layerID)
	if err != nil {
		return PromptLayer{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.readLayerHistoryLocked(normalizedAgentID, normalizedLayerID)
	if err != nil {
		return PromptLayer{}, err
	}

	updated := PromptLayer{
		LayerID:   normalizedLayerID,
		Content:   content,
		UpdatedAt: s.nowFn().UTC(),
		Version:   nextLayerVersion(history),
	}

	history.Current = updated
	history.History = append(history.History, updated)

	if err := s.writeLayerHistoryLocked(normalizedAgentID, normalizedLayerID, history); err != nil {
		return PromptLayer{}, err
	}

	return updated, nil
}

func (s *VersionStore) ListHistory(agentID, layerID string) ([]PromptLayer, error) {
	normalizedAgentID, normalizedLayerID, err := s.normalizeIDs(agentID, layerID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.readLayerHistoryLocked(normalizedAgentID, normalizedLayerID)
	if err != nil {
		return nil, err
	}

	out := make([]PromptLayer, len(history.History))
	copy(out, history.History)
	return out, nil
}

func (s *VersionStore) ListHistoryByLayer(agentID string) (map[string][]PromptLayer, error) {
	normalizedAgentID, err := normalizeAgentID(agentID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string][]PromptLayer, len(layerOrder))
	for _, layerID := range layerOrder {
		history, err := s.readLayerHistoryLocked(normalizedAgentID, layerID)
		if err != nil {
			return nil, err
		}
		entries := make([]PromptLayer, len(history.History))
		copy(entries, history.History)
		out[layerID] = entries
	}

	return out, nil
}

func (s *VersionStore) GetVersion(agentID, layerID string, version int) (PromptLayer, error) {
	_, normalizedLayerID, history, err := s.getLayerHistory(agentID, layerID)
	if err != nil {
		return PromptLayer{}, err
	}

	for _, candidate := range history.History {
		if candidate.Version == version {
			return candidate, nil
		}
	}

	return PromptLayer{}, fmt.Errorf("%w: layer %q version %d", ErrVersionNotFound, normalizedLayerID, version)
}

func (s *VersionStore) DiffVersions(agentID, layerID string, fromVersion, toVersion int) (VersionDiff, error) {
	_, normalizedLayerID, history, err := s.getLayerHistory(agentID, layerID)
	if err != nil {
		return VersionDiff{}, err
	}

	from, err := findVersion(history.History, normalizedLayerID, fromVersion)
	if err != nil {
		return VersionDiff{}, err
	}
	to, err := findVersion(history.History, normalizedLayerID, toVersion)
	if err != nil {
		return VersionDiff{}, err
	}

	return buildVersionDiff(normalizedLayerID, fromVersion, from.Content, toVersion, to.Content), nil
}

func (s *VersionStore) Rollback(agentID, layerID string, version int) (PromptLayer, error) {
	normalizedAgentID, normalizedLayerID, err := s.normalizeIDs(agentID, layerID)
	if err != nil {
		return PromptLayer{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.readLayerHistoryLocked(normalizedAgentID, normalizedLayerID)
	if err != nil {
		return PromptLayer{}, err
	}

	target, err := findVersion(history.History, normalizedLayerID, version)
	if err != nil {
		return PromptLayer{}, err
	}

	rolledBack := PromptLayer{
		LayerID:   normalizedLayerID,
		Content:   target.Content,
		UpdatedAt: s.nowFn().UTC(),
		Version:   nextLayerVersion(history),
	}

	history.Current = rolledBack
	history.History = append(history.History, rolledBack)

	if err := s.writeLayerHistoryLocked(normalizedAgentID, normalizedLayerID, history); err != nil {
		return PromptLayer{}, err
	}

	return rolledBack, nil
}

func (s *VersionStore) getLayerHistory(agentID, layerID string) (string, string, storedLayerHistory, error) {
	normalizedAgentID, normalizedLayerID, err := s.normalizeIDs(agentID, layerID)
	if err != nil {
		return "", "", storedLayerHistory{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.readLayerHistoryLocked(normalizedAgentID, normalizedLayerID)
	if err != nil {
		return "", "", storedLayerHistory{}, err
	}

	return normalizedAgentID, normalizedLayerID, history, nil
}

func (s *VersionStore) normalizeIDs(agentID, layerID string) (string, string, error) {
	normalizedAgentID, err := normalizeAgentID(agentID)
	if err != nil {
		return "", "", err
	}
	normalizedLayerID := strings.TrimSpace(layerID)
	if !isValidLayerID(normalizedLayerID) {
		return "", "", ErrInvalidLayerID
	}
	return normalizedAgentID, normalizedLayerID, nil
}

func (s *VersionStore) readLayerHistoryLocked(agentID, layerID string) (storedLayerHistory, error) {
	path := s.layerPath(agentID, layerID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return storedLayerHistory{Current: PromptLayer{LayerID: layerID}}, nil
		}
		return storedLayerHistory{}, fmt.Errorf("promptstack: read layer history: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return storedLayerHistory{Current: PromptLayer{LayerID: layerID}}, nil
	}

	var history storedLayerHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return storedLayerHistory{}, fmt.Errorf("promptstack: decode layer history: %w", err)
	}

	history = normalizeStoredLayerHistory(history, layerID)
	return history, nil
}

func (s *VersionStore) writeLayerHistoryLocked(agentID, layerID string, history storedLayerHistory) error {
	history = normalizeStoredLayerHistory(history, layerID)

	encoded, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("promptstack: encode layer history: %w", err)
	}
	encoded = append(encoded, '\n')

	if err := fsutil.WriteFileAtomic(s.layerPath(agentID, layerID), encoded, 0o600); err != nil {
		return fmt.Errorf("promptstack: write layer history: %w", err)
	}

	return nil
}

func (s *VersionStore) layerPath(agentID, layerID string) string {
	if strings.TrimSpace(s.instanceID) != "" {
		return filepath.Join(s.controlPlaneDir, "instances", s.instanceID, "agents", agentID, "promptstack", layerID+".json")
	}
	return filepath.Join(s.controlPlaneDir, "agents", agentID, "promptstack", layerID+".json")
}

func normalizeStoredLayerHistory(history storedLayerHistory, layerID string) storedLayerHistory {
	if history.Current.LayerID == "" {
		history.Current.LayerID = layerID
	}

	normalized := make([]PromptLayer, 0, len(history.History))
	for _, entry := range history.History {
		if entry.LayerID == "" {
			entry.LayerID = layerID
		}
		normalized = append(normalized, entry)
	}

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Version < normalized[j].Version
	})

	history.History = normalized
	if len(history.History) > 0 {
		history.Current = history.History[len(history.History)-1]
	}

	if history.Current.LayerID == "" {
		history.Current.LayerID = layerID
	}

	return history
}

func nextLayerVersion(history storedLayerHistory) int {
	last := history.Current.Version
	if len(history.History) > 0 {
		historyLast := history.History[len(history.History)-1].Version
		if historyLast > last {
			last = historyLast
		}
	}
	return last + 1
}

func findVersion(history []PromptLayer, layerID string, version int) (PromptLayer, error) {
	for _, candidate := range history {
		if candidate.Version == version {
			return candidate, nil
		}
	}
	return PromptLayer{}, fmt.Errorf("%w: layer %q version %d", ErrVersionNotFound, layerID, version)
}

func normalizeAgentID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		id = "default"
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\\`) {
		return "", ErrInvalidAgentID
	}
	for _, r := range id {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum || r == '-' || r == '_' {
			continue
		}
		return "", ErrInvalidAgentID
	}
	return id, nil
}
