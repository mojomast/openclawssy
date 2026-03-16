package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/config"
)

const (
	maxInstanceIDLen          = 64
	maxInstanceNameLen        = 80
	maxInstanceDescriptionLen = 240
	maxInstanceTemplateLen    = 80
)

type controlPlaneFeatures struct {
	InstanceControl bool `json:"instance_control"`
	InstanceAgents  bool `json:"instance_agents"`
	Wizard          bool `json:"wizard"`
}

type controlPlaneStore struct {
	ActiveInstanceID string               `json:"active_instance_id,omitempty"`
	Features         controlPlaneFeatures `json:"features"`
	Instances        []dashboardInstance  `json:"instances,omitempty"`
	UpdatedAt        string               `json:"updated_at,omitempty"`
}

type dashboardInstance struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Description      string        `json:"description,omitempty"`
	Template         string        `json:"template,omitempty"`
	Source           string        `json:"source,omitempty"`
	SourceInstanceID string        `json:"source_instance_id,omitempty"`
	CreatedAt        string        `json:"created_at"`
	UpdatedAt        string        `json:"updated_at"`
	Config           config.Config `json:"config"`
}

func defaultControlPlaneFeatures() controlPlaneFeatures {
	return controlPlaneFeatures{
		InstanceControl: true,
		InstanceAgents:  true,
		Wizard:          true,
	}
}

func defaultControlPlaneStore() controlPlaneStore {
	return controlPlaneStore{
		Features:  defaultControlPlaneFeatures(),
		Instances: []dashboardInstance{},
	}
}

func (h *Handler) controlPlaneStorePath() string {
	return filepath.Join(h.rootDir, ".openclawssy", "controlplane", "instances.json")
}

func (h *Handler) loadControlPlaneStore() (controlPlaneStore, error) {
	path := h.controlPlaneStorePath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultControlPlaneStore(), nil
		}
		return controlPlaneStore{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return controlPlaneStore{}, err
	}

	store := defaultControlPlaneStore()
	if err := json.Unmarshal(raw, &store); err != nil {
		return controlPlaneStore{}, fmt.Errorf("parse control plane store: %w", err)
	}
	if len(store.Instances) == 0 {
		store.Instances = []dashboardInstance{}
	}
	for i := range store.Instances {
		store.Instances[i].ID = strings.TrimSpace(store.Instances[i].ID)
		store.Instances[i].Name = strings.TrimSpace(store.Instances[i].Name)
		store.Instances[i].Template = strings.TrimSpace(store.Instances[i].Template)
		store.Instances[i].Description = strings.TrimSpace(store.Instances[i].Description)
		store.Instances[i].Source = strings.TrimSpace(store.Instances[i].Source)
		store.Instances[i].SourceInstanceID = strings.TrimSpace(store.Instances[i].SourceInstanceID)
		store.Instances[i].Config.ApplyDefaults()
		if err := store.Instances[i].Config.Validate(); err != nil {
			return controlPlaneStore{}, fmt.Errorf("control plane instance %q invalid: %w", store.Instances[i].ID, err)
		}
	}
	if store.Features == (controlPlaneFeatures{}) {
		store.Features = defaultControlPlaneFeatures()
	}
	sort.Slice(store.Instances, func(i, j int) bool {
		if store.Instances[i].UpdatedAt == store.Instances[j].UpdatedAt {
			return store.Instances[i].ID < store.Instances[j].ID
		}
		return store.Instances[i].UpdatedAt > store.Instances[j].UpdatedAt
	})
	return store, nil
}

func (h *Handler) saveControlPlaneStore(store controlPlaneStore) error {
	if store.Features == (controlPlaneFeatures{}) {
		store.Features = defaultControlPlaneFeatures()
	}
	if len(store.Instances) == 0 {
		store.Instances = []dashboardInstance{}
	}
	store.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	buf, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal control plane store: %w", err)
	}
	buf = append(buf, '\n')
	return config.WriteAtomic(h.controlPlaneStorePath(), buf, 0o600)
}

func (s controlPlaneStore) instanceByID(instanceID string) (dashboardInstance, int, bool) {
	for i, instance := range s.Instances {
		if instance.ID == instanceID {
			return instance, i, true
		}
	}
	return dashboardInstance{}, -1, false
}

func normalizeInstanceID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", errors.New("instance_id is required")
	}
	if len(id) > maxInstanceIDLen {
		return "", fmt.Errorf("instance_id exceeds %d characters", maxInstanceIDLen)
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\\`) {
		return "", errors.New("invalid instance id")
	}
	for _, r := range id {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum || r == '-' || r == '_' {
			continue
		}
		return "", errors.New("invalid instance id")
	}
	if id == "active" || id == "bootstrap-from-current" {
		return "", errors.New("invalid instance id")
	}
	return id, nil
}

func cloneDashboardConfig(cfg config.Config) (config.Config, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return config.Config{}, fmt.Errorf("marshal config clone: %w", err)
	}
	var cloned config.Config
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return config.Config{}, fmt.Errorf("unmarshal config clone: %w", err)
	}
	cloned.ApplyDefaults()
	return cloned, nil
}

func instanceSummary(instance dashboardInstance, activeInstanceID string) map[string]any {
	agentCount := len(instance.Config.Agents.Profiles)
	return map[string]any{
		"id":                 instance.ID,
		"name":               instance.Name,
		"description":        instance.Description,
		"template":           instance.Template,
		"source":             instance.Source,
		"source_instance_id": instance.SourceInstanceID,
		"created_at":         instance.CreatedAt,
		"updated_at":         instance.UpdatedAt,
		"agent_count":        agentCount,
		"model_provider":     instance.Config.Model.Provider,
		"model_name":         instance.Config.Model.Name,
		"is_active":          instance.ID == activeInstanceID,
	}
}

func instancePayload(instance dashboardInstance, activeInstanceID string) map[string]any {
	payload := instanceSummary(instance, activeInstanceID)
	payload["config"] = instance.Config.Redacted()
	return payload
}
