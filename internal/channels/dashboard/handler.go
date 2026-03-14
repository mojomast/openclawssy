package dashboard

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
	"openclawssy/internal/promptstack"
	"openclawssy/internal/runtime"
	"openclawssy/internal/scheduler"
	"openclawssy/internal/secrets"
	"openclawssy/internal/skillcatalog"
)

type Handler struct {
	rootDir         string
	store           httpchannel.RunStore
	schedulerStore  *scheduler.Store
	runCanceller    dashboardRunCanceller
	monitorRunMu    sync.Mutex
	monitorRuns     map[string]monitorRunState
	promptStackMu   sync.Mutex
	promptStack     *promptstack.VersionStore
	rollbackMu      sync.Mutex
	rollbackByAgent map[string][]agentRollbackSnapshot
}

type dashboardRunCanceller interface {
	Cancel(runID string) error
	IsTracked(runID string) bool
}

type Options struct {
	SchedulerStore *scheduler.Store
	RunCanceller   dashboardRunCanceller
}

type monitorRunRecord struct {
	RunID          string `json:"run_id"`
	AgentID        string `json:"agent_id"`
	TaskID         string `json:"task_id,omitempty"`
	Source         string `json:"source,omitempty"`
	Role           string `json:"role"`
	Message        string `json:"message,omitempty"`
	ModelProvider  string `json:"model_provider,omitempty"`
	ModelName      string `json:"model_name,omitempty"`
	CheckpointPath string `json:"checkpoint_path,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Status         string `json:"status"`
	Tracked        bool   `json:"tracked"`
	Error          string `json:"error,omitempty"`
	ArtifactPath   string `json:"artifact_path,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
}

type monitorRunState struct {
	Status    string
	UpdatedAt time.Time
}

const (
	monitorRunStateTTL            = 15 * time.Minute
	monitorStaleUntrackedRunTTL   = 45 * time.Minute
	monitorStaleUntrackedRunError = "stale untracked run retired"
)

type dashboardLayoutRecord struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Position  int                     `json:"position,omitempty"`
	CreatedAt string                  `json:"created_at"`
	UpdatedAt string                  `json:"updated_at"`
	Layout    []dashboardWidgetLayout `json:"layout"`
}

type dashboardWidgetLayout struct {
	WidgetKey        string         `json:"widget_key"`
	WidgetInstanceID string         `json:"widget_instance_id"`
	X                int            `json:"x"`
	Y                int            `json:"y"`
	W                int            `json:"w"`
	H                int            `json:"h"`
	WidgetState      map[string]any `json:"widget_state,omitempty"`
}

type agentDocPayload struct {
	Name         string `json:"name"`
	ResolvedName string `json:"resolved_name"`
	AliasFor     string `json:"alias_for,omitempty"`
	Content      string `json:"content"`
	Exists       bool   `json:"exists"`
}

type workspaceEntryPayload struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
}

var dashboardEditableDocNames = []string{
	"SOUL.md",
	"RULES.md",
	"TOOLS.md",
	"SPECPLAN.md",
	"DEVPLAN.md",
	"HANDOFF.md",
	"HEARTBEAT.md",
}

const (
	skillsBlockStartTag = "<!-- OPENCLAWSSY_ACTIVATED_SKILLS_START -->"
	skillsBlockEndTag   = "<!-- OPENCLAWSSY_ACTIVATED_SKILLS_END -->"
)

var builtInSkillCatalog = skillcatalog.Catalog()

//go:embed ui/dist
var dashboardUIFS embed.FS

func New(rootDir string, store httpchannel.RunStore, schedulerStore ...*scheduler.Store) *Handler {
	var opts Options
	if len(schedulerStore) > 0 {
		opts.SchedulerStore = schedulerStore[0]
	}
	return NewWithOptions(rootDir, store, opts)
}

func NewWithOptions(rootDir string, store httpchannel.RunStore, opts Options) *Handler {
	return &Handler{
		rootDir:         rootDir,
		store:           store,
		schedulerStore:  opts.SchedulerStore,
		runCanceller:    opts.RunCanceller,
		monitorRuns:     make(map[string]monitorRunState),
		rollbackByAgent: make(map[string][]agentRollbackSnapshot),
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/favicon.ico", h.serveDashboardFavicon)
	mux.HandleFunc("/dashboard", h.serveDashboard)
	mux.HandleFunc("/dashboard/", h.serveDashboard)
	mux.HandleFunc("/dashboard/static/", h.serveDashboardStatic)
	mux.HandleFunc("/api/admin/status", h.getStatus)
	mux.HandleFunc("/api/admin/config", h.handleConfig)
	mux.HandleFunc("/api/admin/config/validate", h.handleConfigValidate)
	mux.HandleFunc("/api/admin/providers/test", h.handleProviderTest)
	mux.HandleFunc("/api/admin/providers/models", h.handleProviderModels)
	mux.HandleFunc("/api/admin/secrets", h.handleSecrets)
	mux.HandleFunc("/api/admin/secrets/", h.handleSecretByKey)
	mux.HandleFunc("/api/admin/dashboards", h.handleDashboards)
	mux.HandleFunc("/api/admin/dashboards/", h.handleDashboardByID)
	mux.HandleFunc("/api/admin/scheduler/jobs", h.handleSchedulerJobs)
	mux.HandleFunc("/api/admin/scheduler/jobs/", h.handleSchedulerJobByID)
	mux.HandleFunc("/api/admin/scheduler/control", h.handleSchedulerControl)
	mux.HandleFunc("/api/admin/chat/sessions", h.listChatSessions)
	mux.HandleFunc("/api/admin/chat/sessions/", h.chatSessionMessages)
	mux.HandleFunc("/api/admin/monitor/runs", h.handleMonitorRuns)
	mux.HandleFunc("/api/admin/monitor/runs/control", h.handleMonitorRunControl)
	mux.HandleFunc("/api/admin/runs/", h.handleRunDecisions)
	mux.HandleFunc("/api/admin/roles", h.handleRoles)
	mux.HandleFunc("/api/admin/roles/", h.handleRoleByName)
	mux.HandleFunc("/api/admin/agents", h.handleAgents)
	mux.HandleFunc("/api/admin/agents/", h.handleAgentContractAPI)
	mux.HandleFunc("/api/admin/agent/docs", h.handleAgentDocs)
	mux.HandleFunc("/api/admin/skills", h.handleSkills)
	mux.HandleFunc("/api/admin/workspace/entries", h.handleWorkspaceEntries)
	mux.HandleFunc("/api/admin/workspace/file", h.handleWorkspaceFile)
	mux.HandleFunc("/api/admin/debug/runs/", h.getRunTrace)
	mux.HandleFunc("/api/admin/memory/", h.getAgentMemory)
}

func (h *Handler) serveDashboardFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const (
	maxAdminSecretKeyLen   = 256
	maxAdminSecretValueLen = 16384
	maxDashboardNameLen    = 80
	maxDashboardsCount     = 24
	maxDashboardWidgets    = 64
	maxWidgetKeyLen        = 80
	maxWidgetStateBytes    = 4096
)

func (h *Handler) schedulerStoreOrDefault() (*scheduler.Store, error) {
	if h.schedulerStore != nil {
		return h.schedulerStore, nil
	}
	return scheduler.NewStore(filepath.Join(h.rootDir, ".openclawssy", "scheduler", "jobs.json"))
}

func (h *Handler) handleSchedulerJobs(w http.ResponseWriter, r *http.Request) {
	store, err := h.schedulerStoreOrDefault()
	if err != nil {
		http.Error(w, "failed to open scheduler store", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"paused": store.IsPaused(), "jobs": store.List()})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID        string `json:"id"`
		AgentID   string `json:"agent_id"`
		Schedule  string `json:"schedule"`
		Message   string `json:"message"`
		Channel   string `json:"channel"`
		UserID    string `json:"user_id"`
		RoomID    string `json:"room_id"`
		SessionID string `json:"session_id"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	req.Schedule = strings.TrimSpace(req.Schedule)
	req.Message = strings.TrimSpace(req.Message)
	if req.Schedule == "" || req.Message == "" {
		http.Error(w, "schedule and message are required", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = "job_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = "default"
	}
	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = "dashboard"
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "dashboard_user"
	}
	roomID := strings.TrimSpace(req.RoomID)
	if roomID == "" {
		roomID = "dashboard"
	}
	sessionID := strings.TrimSpace(req.SessionID)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := store.Add(scheduler.Job{ID: id, AgentID: agentID, Schedule: req.Schedule, Message: req.Message, Channel: channel, UserID: userID, RoomID: roomID, SessionID: sessionID, Enabled: enabled}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

func (h *Handler) handleSchedulerJobByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store, err := h.schedulerStoreOrDefault()
	if err != nil {
		http.Error(w, "failed to open scheduler store", http.StatusInternalServerError)
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/scheduler/jobs/"))
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}
	if err := store.Remove(id); err != nil {
		if errors.Is(err, scheduler.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "removed": id})
}

func (h *Handler) handleSchedulerControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store, err := h.schedulerStoreOrDefault()
	if err != nil {
		http.Error(w, "failed to open scheduler store", http.StatusInternalServerError)
		return
	}
	var req struct {
		Action string `json:"action"`
		JobID  string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "pause" && action != "resume" {
		http.Error(w, "action must be pause or resume", http.StatusBadRequest)
		return
	}
	jobID := strings.TrimSpace(req.JobID)
	enable := action == "resume"
	if jobID != "" {
		if err := store.SetJobEnabled(jobID, enable); err != nil {
			if errors.Is(err, scheduler.ErrJobNotFound) {
				http.Error(w, "job not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "action": action, "job_id": jobID})
		return
	}
	if err := store.SetPaused(action == "pause"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "action": action, "paused": store.IsPaused()})
}

func (h *Handler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/dashboard" && r.URL.Path != "/dashboard/" {
		http.NotFound(w, r)
		return
	}
	content, err := dashboardUIFS.ReadFile("ui/dist/index.html")
	if err != nil {
		http.Error(w, "dashboard ui not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Prevent caching of index.html to ensure fresh SPA on reload
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(content)
}
func (h *Handler) serveDashboardStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	assetPath := strings.TrimPrefix(r.URL.Path, "/dashboard/static/")
	assetPath = path.Clean(strings.TrimSpace(assetPath))
	if assetPath == "" || assetPath == "." || strings.HasPrefix(assetPath, "../") {
		http.NotFound(w, r)
		return
	}

	content, err := dashboardUIFS.ReadFile("ui/dist/" + assetPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ext := filepath.Ext(assetPath)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" && strings.HasSuffix(assetPath, ".md") {
		contentType = "text/markdown; charset=utf-8"
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Add cache headers for static assets with hash in filename (e.g., index-abc123.js)
	// These files are immutable and can be cached indefinitely
	if strings.Contains(assetPath, "assets/") && (strings.Contains(assetPath, "-") || strings.Contains(assetPath, ".")) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// For other static assets, use a shorter cache time
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	_, _ = w.Write(content)
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runs, err := h.store.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := map[string]any{
		"run_count": len(runs),
		"runs":      runs,
		"model": map[string]any{
			"provider": cfg.Model.Provider,
			"name":     cfg.Model.Name,
		},
		"discord_enabled":  cfg.Discord.Enabled,
		"telegram_enabled": cfg.Telegram.Enabled,
	}
	writeJSON(w, out)
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(h.rootDir, ".openclawssy", "config.json")
	if r.Method == http.MethodGet {
		cfg, err := config.LoadOrDefault(path)
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
			return
		}
		writeJSON(w, cfg.Redacted())
		return
	}
	switch r.Method {
	case http.MethodPost:
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeDashboardError(w, http.StatusBadRequest, "config.invalid_json", "invalid json body", nil)
			return
		}
		if err := saveDashboardConfig(path, cfg); err != nil {
			writeConfigValidationError(w, err, cfg)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	case http.MethodPatch:
		current, err := config.LoadOrDefault(path)
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
			return
		}
		merged, err := mergeConfigPatch(current, r.Body)
		if err != nil {
			writeDashboardError(w, http.StatusBadRequest, "config.invalid_patch", err.Error(), nil)
			return
		}
		if err := saveDashboardConfig(path, merged); err != nil {
			writeConfigValidationError(w, err, merged)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "config": merged.Redacted()})
		return
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	current, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}
	merged, err := mergeConfigPatch(current, r.Body)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "config.invalid_patch", err.Error(), nil)
		return
	}
	if err := merged.Validate(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "message": err.Error(), "field_errors": collectConfigFieldErrors(merged, err)})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "field_errors": map[string]string{}})
}

func (h *Handler) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDashboardError(w, http.StatusBadRequest, "provider_test.invalid_json", "invalid json body", nil)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	baseURL := strings.TrimSpace(req.BaseURL)
	if provider == "" || baseURL == "" {
		writeDashboardError(w, http.StatusBadRequest, "provider_test.invalid_input", "provider and base_url are required", nil)
		return
	}
	if !map[string]bool{"openai": true, "openrouter": true, "requesty": true, "hatz": true, "zai": true, "generic": true}[provider] {
		writeDashboardError(w, http.StatusBadRequest, "provider_test.invalid_provider", "provider must be one of openai, openrouter, requesty, hatz, zai, generic", nil)
		return
	}
	client := &http.Client{Timeout: 4 * time.Second}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodHead, baseURL, nil)
	if err != nil {
		writeDashboardError(w, http.StatusBadRequest, "provider_test.invalid_url", err.Error(), nil)
		return
	}
	resp, err := client.Do(request)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "provider": provider, "base_url": baseURL, "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	writeJSON(w, map[string]any{
		"ok":          resp.StatusCode < 500,
		"provider":    provider,
		"base_url":    baseURL,
		"status":      resp.StatusCode,
		"status_text": resp.Status,
	})
}

func (h *Handler) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		writeDashboardError(w, http.StatusBadRequest, "provider_models.invalid_provider", "provider is required", nil)
		return
	}
	if !map[string]bool{"openai": true, "openrouter": true, "requesty": true, "hatz": true, "zai": true, "generic": true}[provider] {
		writeDashboardError(w, http.StatusBadRequest, "provider_models.invalid_provider", "provider must be one of openai, openrouter, requesty, hatz, zai, generic", nil)
		return
	}

	cfg, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "config.load_failed", err.Error(), nil)
		return
	}
	secretStore, err := secrets.NewStore(cfg)
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "provider_models.secret_store_failed", err.Error(), nil)
		return
	}
	models, err := runtime.ListProviderModels(r.Context(), cfg, provider, func(name string) (string, bool, error) {
		return secretStore.Get(name)
	})
	if err != nil {
		writeDashboardError(w, http.StatusBadGateway, "provider_models.fetch_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"provider": provider, "models": models, "count": len(models)})
}

func (h *Handler) handleDashboards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		dashboards, err := h.loadDashboardLayouts()
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "dashboards.load_failed", err.Error(), nil)
			return
		}
		writeJSON(w, map[string]any{"dashboards": dashboards})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeDashboardError(w, http.StatusBadRequest, "dashboards.invalid_json", "invalid json body", nil)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = "New Dashboard"
		}
		if len(name) > maxDashboardNameLen {
			writeDashboardError(w, http.StatusBadRequest, "dashboards.invalid_name", fmt.Sprintf("dashboard name exceeds %d characters", maxDashboardNameLen), nil)
			return
		}
		dashboards, err := h.loadDashboardLayouts()
		if err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "dashboards.load_failed", err.Error(), nil)
			return
		}
		if len(dashboards) >= maxDashboardsCount {
			writeDashboardError(w, http.StatusBadRequest, "dashboards.limit_reached", fmt.Sprintf("dashboard limit reached (%d)", maxDashboardsCount), nil)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		created := dashboardLayoutRecord{ID: "dash_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10), Name: name, Position: len(dashboards), CreatedAt: now, UpdatedAt: now, Layout: []dashboardWidgetLayout{}}
		dashboards = append(dashboards, created)
		if err := h.saveDashboardLayouts(dashboards); err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "dashboards.save_failed", err.Error(), nil)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "dashboard": created})
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func (h *Handler) handleDashboardByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/dashboards/"))
	if id == "" || strings.Contains(id, "/") {
		writeDashboardError(w, http.StatusBadRequest, "dashboards.invalid_id", "invalid dashboard id", nil)
		return
	}
	dashboards, err := h.loadDashboardLayouts()
	if err != nil {
		writeDashboardError(w, http.StatusInternalServerError, "dashboards.load_failed", err.Error(), nil)
		return
	}
	index := -1
	for i := range dashboards {
		if dashboards[i].ID == id {
			index = i
			break
		}
	}
	switch r.Method {
	case http.MethodPut:
		if index < 0 {
			writeDashboardError(w, http.StatusNotFound, "dashboards.not_found", "dashboard not found", nil)
			return
		}
		var req dashboardLayoutRecord
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeDashboardError(w, http.StatusBadRequest, "dashboards.invalid_json", "invalid json body", nil)
			return
		}
		updated := dashboards[index]
		if name := strings.TrimSpace(req.Name); name != "" {
			updated.Name = name
		}
		if len(updated.Name) > maxDashboardNameLen {
			writeDashboardError(w, http.StatusBadRequest, "dashboards.invalid_name", fmt.Sprintf("dashboard name exceeds %d characters", maxDashboardNameLen), nil)
			return
		}
		if req.Position >= 0 {
			updated.Position = req.Position
		}
		updated.Layout = normalizeDashboardLayout(req.Layout)
		if err := validateDashboardRecord(updated); err != nil {
			writeDashboardError(w, http.StatusBadRequest, "dashboards.invalid_layout", err.Error(), nil)
			return
		}
		updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		dashboards[index] = updated
		for i := range dashboards {
			if i != index && dashboards[i].Position >= updated.Position {
				dashboards[i].Position = i
			}
		}
		if err := h.saveDashboardLayouts(dashboards); err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "dashboards.save_failed", err.Error(), nil)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "dashboard": updated})
	case http.MethodDelete:
		if index < 0 {
			writeDashboardError(w, http.StatusNotFound, "dashboards.not_found", "dashboard not found", nil)
			return
		}
		dashboards = append(dashboards[:index], dashboards[index+1:]...)
		for i := range dashboards {
			dashboards[i].Position = i
		}
		if err := h.saveDashboardLayouts(dashboards); err != nil {
			writeDashboardError(w, http.StatusInternalServerError, "dashboards.save_failed", err.Error(), nil)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "deleted": id})
	default:
		writeDashboardError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed", nil)
	}
}

func normalizeDashboardLayout(layout []dashboardWidgetLayout) []dashboardWidgetLayout {
	out := make([]dashboardWidgetLayout, 0, len(layout))
	for _, item := range layout {
		if strings.TrimSpace(item.WidgetKey) == "" || strings.TrimSpace(item.WidgetInstanceID) == "" {
			continue
		}
		if item.W < 1 {
			item.W = 3
		}
		if item.H < 1 {
			item.H = 2
		}
		if item.X < 0 {
			item.X = 0
		}
		if item.Y < 0 {
			item.Y = 0
		}
		out = append(out, item)
	}
	return out
}

func validateDashboardRecord(record dashboardLayoutRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return errors.New("dashboard id is required")
	}
	if strings.TrimSpace(record.Name) == "" {
		return errors.New("dashboard name is required")
	}
	if len(record.Name) > maxDashboardNameLen {
		return fmt.Errorf("dashboard name exceeds %d characters", maxDashboardNameLen)
	}
	if len(record.Layout) > maxDashboardWidgets {
		return fmt.Errorf("dashboard has too many widgets (max %d)", maxDashboardWidgets)
	}
	seen := map[string]bool{}
	for _, item := range record.Layout {
		if err := validateDashboardWidget(item); err != nil {
			return err
		}
		if seen[item.WidgetInstanceID] {
			return fmt.Errorf("duplicate widget_instance_id: %s", item.WidgetInstanceID)
		}
		seen[item.WidgetInstanceID] = true
	}
	return nil
}

func validateDashboardWidget(item dashboardWidgetLayout) error {
	if key := strings.TrimSpace(item.WidgetKey); key == "" || len(key) > maxWidgetKeyLen {
		return fmt.Errorf("invalid widget_key: %q", item.WidgetKey)
	}
	if id := strings.TrimSpace(item.WidgetInstanceID); id == "" || len(id) > maxWidgetKeyLen {
		return fmt.Errorf("invalid widget_instance_id: %q", item.WidgetInstanceID)
	}
	if item.X < 0 || item.Y < 0 {
		return errors.New("widget position must be >= 0")
	}
	if item.W < 1 || item.W > 12 {
		return errors.New("widget width must be between 1 and 12")
	}
	if item.H < 1 || item.H > 12 {
		return errors.New("widget height must be between 1 and 12")
	}
	if item.WidgetState != nil {
		data, err := json.Marshal(item.WidgetState)
		if err != nil {
			return errors.New("widget_state must be valid json")
		}
		if len(data) > maxWidgetStateBytes {
			return fmt.Errorf("widget_state exceeds %d bytes", maxWidgetStateBytes)
		}
	}
	return nil
}

func (h *Handler) dashboardLayoutsPath() string {
	return filepath.Join(h.rootDir, ".openclawssy", "dashboard_layouts.json")
}

func (h *Handler) loadDashboardLayouts() ([]dashboardLayoutRecord, error) {
	path := h.dashboardLayoutsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []dashboardLayoutRecord{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []dashboardLayoutRecord{}, nil
	}
	var dashboards []dashboardLayoutRecord
	if err := json.Unmarshal(data, &dashboards); err != nil {
		return nil, err
	}
	for i := range dashboards {
		dashboards[i].Layout = normalizeDashboardLayout(dashboards[i].Layout)
	}
	sort.SliceStable(dashboards, func(i, j int) bool {
		if dashboards[i].Position != dashboards[j].Position {
			return dashboards[i].Position < dashboards[j].Position
		}
		return dashboards[i].CreatedAt < dashboards[j].CreatedAt
	})
	return dashboards, nil
}

func (h *Handler) saveDashboardLayouts(dashboards []dashboardLayoutRecord) error {
	if len(dashboards) > maxDashboardsCount {
		return fmt.Errorf("dashboard limit reached (%d)", maxDashboardsCount)
	}
	for i := range dashboards {
		dashboards[i].Position = i
		if err := validateDashboardRecord(dashboards[i]); err != nil {
			return err
		}
	}
	path := h.dashboardLayoutsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(dashboards, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func (h *Handler) handleSecrets(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	store, err := secrets.NewStore(cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		keys, err := store.ListKeys()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sort.Strings(keys)
		writeJSON(w, map[string]any{"keys": keys})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		name, err := validateAdminSecretKey(req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateAdminSecretValue(req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.Set(name, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "stored": name})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *Handler) handleSecretByKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rawKey := strings.TrimPrefix(r.URL.Path, "/api/admin/secrets/")
	if rawKey == "" {
		http.Error(w, "secret key is required", http.StatusBadRequest)
		return
	}
	decodedKey, err := url.PathUnescape(rawKey)
	if err != nil {
		http.Error(w, "invalid secret key path", http.StatusBadRequest)
		return
	}
	key, err := validateAdminSecretKey(decodedKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadOrDefault(filepath.Join(h.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	store, err := secrets.NewStore(cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deleted, err := store.Delete(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "secret key not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "deleted": key})
}

func validateAdminSecretKey(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > maxAdminSecretKeyLen {
		return "", fmt.Errorf("name exceeds %d characters", maxAdminSecretKeyLen)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain control characters")
		}
	}
	return name, nil
}

func validateAdminSecretValue(value string) error {
	if value == "" {
		return errors.New("value is required")
	}
	if len(value) > maxAdminSecretValueLen {
		return fmt.Errorf("value exceeds %d characters", maxAdminSecretValueLen)
	}
	return nil
}

func saveDashboardConfig(path string, cfg config.Config) error {
	cfg.ApplyDefaults()
	return config.Save(path, cfg)
}

func mergeConfigPatch(current config.Config, body any) (config.Config, error) {
	var patch map[string]any
	switch raw := body.(type) {
	case *strings.Reader:
		_ = raw
	case interface{ Read([]byte) (int, error) }:
		if err := json.NewDecoder(raw).Decode(&patch); err != nil {
			return config.Config{}, errors.New("invalid json body")
		}
	default:
		return config.Config{}, errors.New("invalid patch body")
	}
	if patch == nil {
		patch = map[string]any{}
	}
	baseBytes, err := json.Marshal(current)
	if err != nil {
		return config.Config{}, err
	}
	base := map[string]any{}
	if err := json.Unmarshal(baseBytes, &base); err != nil {
		return config.Config{}, err
	}
	merged := mergeJSONObjects(base, patch)
	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return config.Config{}, err
	}
	var cfg config.Config
	if err := json.Unmarshal(mergedBytes, &cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func mergeJSONObjects(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range patch {
		if value == nil {
			delete(base, key)
			continue
		}
		patchMap, patchIsMap := value.(map[string]any)
		baseMap, baseIsMap := base[key].(map[string]any)
		if patchIsMap {
			if !baseIsMap {
				baseMap = map[string]any{}
			}
			base[key] = mergeJSONObjects(baseMap, patchMap)
			continue
		}
		base[key] = value
	}
	return base
}

func collectConfigFieldErrors(cfg config.Config, err error) map[string]string {
	fieldErrors := map[string]string{}
	set := func(path, message string) {
		if path == "" || message == "" {
			return
		}
		if _, ok := fieldErrors[path]; !ok {
			fieldErrors[path] = message
		}
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.Model.Provider))
	if provider == "" || !map[string]bool{"openai": true, "openrouter": true, "requesty": true, "hatz": true, "zai": true, "generic": true}[provider] {
		set("model.provider", "Provider must be one of openai, openrouter, requesty, hatz, zai, generic.")
	}
	if strings.TrimSpace(cfg.Model.Name) == "" {
		set("model.name", "Model name is required.")
	}
	if cfg.Model.MaxTokens < 1 || cfg.Model.MaxTokens > 20000 {
		set("model.max_tokens", "Max tokens must be an integer between 1 and 20000.")
	}
	if cfg.Model.TimeoutMS < 1000 || cfg.Model.TimeoutMS > 600000 {
		set("model.timeout_ms", "Model timeout must be an integer between 1000 and 600000 ms.")
	}
	if strings.TrimSpace(cfg.Server.BindAddress) == "" {
		set("server.bind_address", "Server bind address is required.")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		set("server.port", "Server port must be an integer between 1 and 65535.")
	}
	if strings.TrimSpace(cfg.Workspace.Root) == "" {
		set("workspace.root", "Workspace root is required.")
	}
	if cfg.Chat.RateLimitPerMin < 1 {
		set("chat.rate_limit_per_min", "Chat rate limit must be an integer >= 1.")
	}
	if cfg.Chat.GlobalRateLimitPerMin < 1 {
		set("chat.global_rate_limit_per_min", "Global chat rate limit must be an integer >= 1.")
	}
	if cfg.Discord.RateLimitPerMin < 1 {
		set("discord.rate_limit_per_min", "Discord rate limit must be an integer >= 1.")
	}
	if cfg.Telegram.RateLimitPerMin < 1 {
		set("telegram.rate_limit_per_min", "Telegram rate limit must be an integer >= 1.")
	}
	if cfg.Sandbox.Active && strings.EqualFold(strings.TrimSpace(cfg.Sandbox.Provider), "none") {
		set("sandbox.provider", "Sandbox provider must be local or docker when sandbox is active.")
	}
	if cfg.Shell.EnableExec && !cfg.Sandbox.Active {
		set("shell.enable_exec", "Shell execution requires sandbox.active=true.")
	}
	for agentID, profile := range cfg.Agents.Profiles {
		provider := strings.ToLower(strings.TrimSpace(profile.Model.Provider))
		if provider != "" && !map[string]bool{"openai": true, "openrouter": true, "requesty": true, "hatz": true, "zai": true, "generic": true}[provider] {
			set("agents.profiles."+agentID+".model.provider", "Profile model provider must match a supported provider.")
		}
		if profile.Model.MaxTokens < 0 || profile.Model.MaxTokens > 20000 {
			set("agents.profiles."+agentID+".model.max_tokens", "Profile max tokens must be an integer between 0 and 20000.")
		}
		if profile.Model.TimeoutMS < 0 || profile.Model.TimeoutMS > 600000 {
			set("agents.profiles."+agentID+".model.timeout_ms", "Profile model timeout must be an integer between 0 and 600000 ms.")
		}
	}
	if cfg.Agents.SubAgentDefaults.MaxToolIterations < 0 {
		set("agents.subagent_defaults.max_tool_iterations", "Subagent max tool iterations must be >= 0.")
	}
	if cfg.Agents.SubAgentDefaults.TimeoutMS < 0 {
		set("agents.subagent_defaults.timeout_ms", "Subagent timeout must be >= 0.")
	}
	if mode := strings.TrimSpace(cfg.Agents.SubAgentDefaults.ThinkingMode); mode != "" && !config.IsValidThinkingMode(mode) {
		set("agents.subagent_defaults.thinking_mode", "Thinking mode must be one of never, on_error, always.")
	}
	validDelegation := map[string]bool{"": true, "prompt_only": true, "tool_gated": true, "auto_execute": true, "suggest_only": true, "approve_plan": true, "auto_trusted": true, "full_autonomous": true}
	if !validDelegation[strings.TrimSpace(cfg.Agents.DelegationMode)] {
		set("agents.delegation_mode", "Delegation mode must be one of prompt_only, tool_gated, auto_execute, suggest_only, approve_plan, auto_trusted, full_autonomous.")
	}
	if !validDelegation[strings.TrimSpace(cfg.Agents.SubAgentDefaults.DelegationMode)] {
		set("agents.subagent_defaults.delegation_mode", "Delegation mode must be one of prompt_only, tool_gated, auto_execute, suggest_only, approve_plan, auto_trusted, full_autonomous.")
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "generic provider base url") && strings.TrimSpace(cfg.Providers.Generic.BaseURL) == "" {
		set("providers.generic.base_url", "Generic provider base URL is required when model.provider is generic.")
	}
	if len(fieldErrors) == 0 {
		set("config", err.Error())
	}
	return fieldErrors
}

func writeConfigValidationError(w http.ResponseWriter, err error, cfg config.Config) {
	writeDashboardError(w, http.StatusBadRequest, "config.validation_failed", err.Error(), map[string]any{"field_errors": collectConfigFieldErrors(cfg, err)})
}

func writeDashboardError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	payload := map[string]any{"error": map[string]any{"code": code, "message": message}}
	if len(details) > 0 {
		payload["error"].(map[string]any)["details"] = details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) getRunTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimPrefix(r.URL.Path, "/api/admin/debug/runs/")
	if suffix == r.URL.Path || !strings.HasSuffix(suffix, "/trace") {
		http.NotFound(w, r)
		return
	}
	runID := strings.TrimSpace(strings.TrimSuffix(suffix, "/trace"))
	if runID == "" || strings.Contains(runID, "/") {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	run, err := h.store.Get(r.Context(), runID)
	if err != nil {
		if errors.Is(err, httpchannel.ErrRunNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load run", http.StatusInternalServerError)
		return
	}
	if len(run.Trace) == 0 {
		http.Error(w, "trace not available for run", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]any{
		"run_id": run.ID,
		"trace":  run.Trace,
	})
}

func (h *Handler) getAgentMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimPrefix(r.URL.Path, "/api/admin/memory/")
	if suffix == r.URL.Path {
		http.NotFound(w, r)
		return
	}
	agentID, err := normalizeDashboardAgentID(suffix)
	if err != nil || strings.Contains(suffix, "/") {
		http.Error(w, "invalid agent id", http.StatusBadRequest)
		return
	}

	dbPath := filepath.Join(h.rootDir, ".openclawssy", "agents", agentID, "memory", "memory.db")
	store, err := memorystore.OpenSQLite(dbPath, agentID)
	if err != nil {
		http.Error(w, "failed to open memory store", http.StatusInternalServerError)
		return
	}
	defer func() { _ = store.Close() }()

	health, err := store.Health(r.Context())
	if err != nil {
		http.Error(w, "failed to load memory health", http.StatusInternalServerError)
		return
	}
	vectorCount, activeVectorCount, models, err := store.EmbeddingStats(r.Context())
	if err != nil {
		http.Error(w, "failed to load embedding stats", http.StatusInternalServerError)
		return
	}
	activeItems, err := store.Search(r.Context(), memory.SearchParams{Limit: 20, MinImportance: 1, Status: memory.MemoryStatusActive})
	if err != nil {
		http.Error(w, "failed to load memory items", http.StatusInternalServerError)
		return
	}
	cfgPath := filepath.Join(h.rootDir, ".openclawssy", "config.json")
	cfg, _ := config.LoadOrDefault(cfgPath)
	coverageRatio := 0.0
	if health.ActiveItems > 0 {
		coverageRatio = float64(activeVectorCount) / float64(health.ActiveItems)
	}

	writeJSON(w, map[string]any{
		"agent_id":       agentID,
		"health":         health,
		"active_items":   activeItems,
		"active_count":   len(activeItems),
		"memory_enabled": true,
		"embedding_stats": map[string]any{
			"enabled":                   cfg.Memory.Enabled && cfg.Memory.EmbeddingsEnabled,
			"provider":                  cfg.Memory.EmbeddingProvider,
			"model":                     cfg.Memory.EmbeddingModel,
			"vector_count":              vectorCount,
			"active_vector_count":       activeVectorCount,
			"active_coverage_ratio":     coverageRatio,
			"semantic_search_available": cfg.Memory.Enabled && cfg.Memory.EmbeddingsEnabled && activeVectorCount > 0,
			"models":                    models,
		},
	})
}

func (h *Handler) listChatSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		http.Error(w, "failed to open chat store", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	agentID := strings.TrimSpace(q.Get("agent_id"))
	if agentID == "" {
		agentID = "default"
	}
	userID := strings.TrimSpace(q.Get("user_id"))
	if userID == "" {
		userID = "dashboard_user"
	}
	roomID := strings.TrimSpace(q.Get("room_id"))
	if roomID == "" {
		roomID = "dashboard"
	}
	channel := strings.TrimSpace(q.Get("channel"))
	if channel == "" {
		channel = "dashboard"
	}

	sessions, err := store.ListSessions(agentID, userID, roomID, channel)
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}

	limit, offset, err := parseLimitOffset(q, 50, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	total := len(sessions)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, map[string]any{
		"sessions": sessions[offset:end],
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

func (h *Handler) chatSessionMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimPrefix(r.URL.Path, "/api/admin/chat/sessions/")
	if suffix == r.URL.Path || !strings.HasSuffix(suffix, "/messages") {
		http.NotFound(w, r)
		return
	}
	sessionID := strings.TrimSpace(strings.TrimSuffix(suffix, "/messages"))
	if sessionID == "" || strings.Contains(sessionID, "/") {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	limit := 200
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 1000 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		http.Error(w, "failed to open chat store", http.StatusInternalServerError)
		return
	}
	msgs, err := store.ReadRecentMessages(sessionID, limit)
	if err != nil {
		if errors.Is(err, chatstore.ErrSessionNotFound) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load messages", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"session_id": sessionID, "messages": msgs})
}

func (h *Handler) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store, err := chatstore.NewStore(filepath.Join(h.rootDir, ".openclawssy", "agents"))
	if err != nil {
		http.Error(w, "failed to open chat store", http.StatusInternalServerError)
		return
	}

	channel := ""
	userID := ""
	roomID := ""
	selectedAgentID := ""
	targetAgentID := ""
	if r.Method == http.MethodGet {
		channel = strings.TrimSpace(r.URL.Query().Get("channel"))
		userID = strings.TrimSpace(r.URL.Query().Get("user_id"))
		roomID = strings.TrimSpace(r.URL.Query().Get("room_id"))
		selectedAgentID = strings.TrimSpace(r.URL.Query().Get("agent_id"))
	} else {
		var req struct {
			Channel string `json:"channel"`
			UserID  string `json:"user_id"`
			RoomID  string `json:"room_id"`
			AgentID string `json:"agent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		channel = strings.TrimSpace(req.Channel)
		userID = strings.TrimSpace(req.UserID)
		roomID = strings.TrimSpace(req.RoomID)
		targetAgentID = strings.TrimSpace(req.AgentID)
		selectedAgentID = targetAgentID
	}

	if channel == "" {
		channel = "dashboard"
	}
	if userID == "" {
		userID = "dashboard_user"
	}
	if roomID == "" {
		roomID = "dashboard"
	}

	if r.Method == http.MethodPost {
		normalizedAgentID, normErr := normalizeDashboardAgentID(targetAgentID)
		if normErr != nil {
			http.Error(w, normErr.Error(), http.StatusBadRequest)
			return
		}
		if err := store.SetActiveAgentPointer(channel, userID, roomID, normalizedAgentID); err != nil {
			http.Error(w, "failed to set active agent", http.StatusInternalServerError)
			return
		}
		targetAgentID = normalizedAgentID
		selectedAgentID = normalizedAgentID
	}

	if selectedAgentID != "" {
		normalizedAgentID, normErr := normalizeDashboardAgentID(selectedAgentID)
		if normErr != nil {
			http.Error(w, normErr.Error(), http.StatusBadRequest)
			return
		}
		selectedAgentID = normalizedAgentID
	}

	active := ""
	if agentID, err := store.GetActiveAgentPointer(channel, userID, roomID); err == nil {
		active = agentID
	}
	if active == "" {
		active = targetAgentID
	}
	if selectedAgentID == "" {
		selectedAgentID = active
	}
	if selectedAgentID == "" {
		selectedAgentID = "default"
	}

	profileContext := map[string]any{
		"agent_id":         selectedAgentID,
		"exists":           false,
		"enabled":          true,
		"self_improvement": false,
		"model_provider":   "",
		"model_name":       "",
		"model_max_tokens": 0,
		"model_timeout_ms": 0,
		"model_override":   false,
	}
	agentsConfig := map[string]any{}
	agentSummaries := map[string]any{}
	cfgPath := filepath.Join(h.rootDir, ".openclawssy", "config.json")
	if cfg, err := config.LoadOrDefault(cfgPath); err == nil {
		agentsConfig = map[string]any{
			"allow_inter_agent_messaging": cfg.Agents.AllowInterAgentMessaging,
			"allow_agent_model_overrides": cfg.Agents.AllowAgentModelOverrides,
			"self_improvement_enabled":    cfg.Agents.SelfImprovementEnabled,
			"enabled_agent_ids":           cfg.Agents.EnabledAgentIDs,
		}
		if profile, ok := cfg.Agents.Profiles[selectedAgentID]; ok {
			profileContext["exists"] = true
			if profile.Enabled != nil {
				profileContext["enabled"] = *profile.Enabled
			}
			profileContext["self_improvement"] = profile.SelfImprovement
			if provider := strings.TrimSpace(profile.Model.Provider); provider != "" {
				profileContext["model_provider"] = provider
				profileContext["model_override"] = true
			}
			if name := strings.TrimSpace(profile.Model.Name); name != "" {
				profileContext["model_name"] = name
				profileContext["model_override"] = true
			}
			if profile.Model.MaxTokens > 0 {
				profileContext["model_max_tokens"] = profile.Model.MaxTokens
				profileContext["model_override"] = true
			}
			if profile.Model.TimeoutMS > 0 {
				profileContext["model_timeout_ms"] = profile.Model.TimeoutMS
				profileContext["model_override"] = true
			}
		}
		agentSummaries = h.buildDashboardAgentSummaries(cfg)
	}
	writeJSON(w, map[string]any{
		"agents":          h.listDashboardAgentIDs(),
		"active_agent":    active,
		"selected_agent":  selectedAgentID,
		"channel":         channel,
		"user_id":         userID,
		"room_id":         roomID,
		"profile_context": profileContext,
		"agents_config":   agentsConfig,
		"agent_summaries": agentSummaries,
	})
}

func (h *Handler) buildDashboardAgentSummaries(cfg config.Config) map[string]any {
	agentIDs := h.listDashboardAgentIDs()
	summaries := make(map[string]any, len(agentIDs))
	for _, agentID := range agentIDs {
		summary := map[string]any{
			"agent_id":                agentID,
			"exists":                  false,
			"enabled":                 true,
			"self_improvement":        false,
			"self_improvement_ready":  false,
			"model_provider":          "",
			"model_name":              "",
			"model_override":          false,
			"activated_skills":        []string{},
			"is_clawdefuckifier":      strings.HasPrefix(strings.ToLower(strings.TrimSpace(agentID)), "clawdefuckifier"),
			"inter_agent_messaging":   cfg.Agents.AllowInterAgentMessaging,
			"global_self_improvement": cfg.Agents.SelfImprovementEnabled,
		}
		if profile, ok := cfg.Agents.Profiles[agentID]; ok {
			summary["exists"] = true
			if profile.Enabled != nil {
				summary["enabled"] = *profile.Enabled
			}
			summary["self_improvement"] = profile.SelfImprovement
			summary["self_improvement_ready"] = cfg.Agents.SelfImprovementEnabled && profile.SelfImprovement
			if provider := strings.TrimSpace(profile.Model.Provider); provider != "" {
				summary["model_provider"] = provider
				summary["model_override"] = true
			}
			if modelName := strings.TrimSpace(profile.Model.Name); modelName != "" {
				summary["model_name"] = modelName
				summary["model_override"] = true
			}
		}
		if activated, err := h.readActivatedSkills(agentID); err == nil && len(activated) > 0 {
			summary["activated_skills"] = activated
		}
		summaries[agentID] = summary
	}
	return summaries
}

func (h *Handler) handleMonitorRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 80
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			http.Error(w, "limit must be between 1 and 500", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	agentFilter := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	runs, err := h.collectMonitorRuns(r.Context(), limit, agentFilter)
	if err != nil {
		http.Error(w, "failed to load monitor runs", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"runs":             runs,
		"available_agents": h.listDashboardAgentIDs(),
	})
}

func (h *Handler) handleMonitorRunControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
		RunID  string `json:"run_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if strings.ToLower(strings.TrimSpace(req.Action)) != "cancel" {
		http.Error(w, "action must be cancel", http.StatusBadRequest)
		return
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" || strings.Contains(runID, "/") {
		http.Error(w, "run_id is required", http.StatusBadRequest)
		return
	}
	if h.runCanceller == nil {
		http.Error(w, "run canceller not configured", http.StatusNotImplemented)
		return
	}
	tracked := h.runCanceller.IsTracked(runID)
	if err := h.runCanceller.Cancel(runID); err != nil {
		if status, ok := h.monitorRunStatusFromStore(r.Context(), runID); ok && isTerminalMonitorRunStatus(status) {
			h.noteMonitorRunState(runID, status)
			writeJSON(w, map[string]any{"run_id": runID, "cancelled": false, "tracked": tracked})
			return
		}
		if !tracked {
			h.noteMonitorRunState(runID, "canceled")
			writeJSON(w, map[string]any{"run_id": runID, "cancelled": true, "tracked": false})
			return
		}
		writeJSON(w, map[string]any{"run_id": runID, "cancelled": false, "tracked": tracked})
		return
	}
	h.noteMonitorRunState(runID, "canceled")
	writeJSON(w, map[string]any{"run_id": runID, "cancelled": true, "tracked": tracked})
}

func (h *Handler) collectMonitorRuns(ctx context.Context, limit int, agentFilter string) ([]monitorRunRecord, error) {
	agentsDir := filepath.Join(h.rootDir, ".openclawssy", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		entries = nil
	}
	byRunID := map[string]monitorRunRecord{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentID, err := normalizeDashboardAgentID(entry.Name())
		if err != nil {
			continue
		}
		if agentFilter != "" && agentFilter != agentID {
			continue
		}
		if err := h.collectMonitorRunsForAgent(filepath.Join(agentsDir, agentID, "audit", "events.jsonl"), byRunID); err != nil {
			continue
		}
	}
	h.reconcileMonitorRunRecords(ctx, byRunID)
	runs := make([]monitorRunRecord, 0, len(byRunID))
	for _, record := range byRunID {
		runs = append(runs, record)
	}
	sort.Slice(runs, func(i, j int) bool {
		left := firstMonitorTimestamp(runs[i])
		right := firstMonitorTimestamp(runs[j])
		if left == right {
			return runs[i].RunID > runs[j].RunID
		}
		return left > right
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func (h *Handler) collectMonitorRunsForAgent(auditPath string, byRunID map[string]monitorRunRecord) error {
	file, err := os.Open(auditPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return nil
		}
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSpace(line)
		}
		if len(line) > 0 {
			var event struct {
				Timestamp time.Time      `json:"ts"`
				Type      string         `json:"type"`
				RunID     string         `json:"run_id"`
				AgentID   string         `json:"agent_id"`
				Payload   map[string]any `json:"payload"`
			}
			if err := json.Unmarshal(line, &event); err == nil {
				runID := strings.TrimSpace(event.RunID)
				agentID := strings.TrimSpace(event.AgentID)
				if runID != "" && agentID != "" {
					record := byRunID[runID]
					record.RunID = runID
					record.AgentID = agentID
					if h.runCanceller != nil && h.runCanceller.IsTracked(runID) {
						record.Tracked = true
					}
					switch strings.TrimSpace(event.Type) {
					case "run.start":
						record.StartedAt = event.Timestamp.UTC().Format(time.RFC3339)
						record.TaskID = firstStringFromMap(event.Payload, "task_id")
						record.Source = firstStringFromMap(event.Payload, "source")
						record.Message = firstStringFromMap(event.Payload, "message")
						record.ModelProvider = firstStringFromMap(event.Payload, "model_provider")
						record.ModelName = firstStringFromMap(event.Payload, "model_name")
						record.SessionID = firstStringFromMap(event.Payload, "session_id")
						if record.Tracked || record.Status == "" {
							record.Status = "running"
						}
					case "run.end":
						record.CompletedAt = event.Timestamp.UTC().Format(time.RFC3339)
						record.ArtifactPath = firstStringFromMap(event.Payload, "artifact_path")
						record.CheckpointPath = firstStringFromMap(event.Payload, "checkpoint_path")
						record.Error = firstStringFromMap(event.Payload, "error")
						if record.Error != "" {
							if isCanceledMonitorError(record.Error) {
								record.Status = "canceled"
							} else {
								record.Status = "failed"
							}
						} else {
							record.Status = "completed"
						}
					}
					record.Role = classifyMonitorRunRole(record.Source)
					if record.Status == "" {
						record.Status = "running"
					}
					byRunID[runID] = record
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func (h *Handler) reconcileMonitorRunRecords(ctx context.Context, byRunID map[string]monitorRunRecord) {
	if len(byRunID) == 0 && h.store == nil {
		return
	}
	now := time.Now().UTC()
	storedRuns := h.listMonitorStoreRuns(ctx)
	storedByID := make(map[string]httpchannel.Run, len(storedRuns))
	for _, run := range storedRuns {
		runID := strings.TrimSpace(run.ID)
		if runID == "" {
			continue
		}
		storedByID[runID] = run
		if record, ok := byRunID[runID]; ok {
			byRunID[runID] = mergeStoredRunIntoMonitorRecord(record, run)
		}
	}

	states := h.snapshotMonitorRunStates(now)
	for runID, state := range states {
		overrideStatus := normalizeMonitorRunStatus(state.Status)
		if overrideStatus == "" {
			h.clearMonitorRunState(runID)
			continue
		}
		if record, ok := byRunID[runID]; ok {
			if isTerminalMonitorRunStatus(record.Status) {
				h.clearMonitorRunState(runID)
				continue
			}
			record.Status = overrideStatus
			record.Tracked = false
			if record.CompletedAt == "" {
				record.CompletedAt = state.UpdatedAt.UTC().Format(time.RFC3339)
			}
			if record.Error == "" && overrideStatus == "canceled" {
				record.Error = "run canceled"
			}
			byRunID[runID] = record
			continue
		}
		run, ok := storedByID[runID]
		if !ok {
			continue
		}
		record := monitorRecordFromStoredRun(run)
		record.Status = overrideStatus
		record.Tracked = false
		if record.CompletedAt == "" {
			record.CompletedAt = state.UpdatedAt.UTC().Format(time.RFC3339)
		}
		if record.Error == "" && overrideStatus == "canceled" {
			record.Error = "run canceled"
		}
		byRunID[runID] = record
	}

	for runID, record := range byRunID {
		if shouldRetireStaleUntrackedMonitorRun(record, storedByID, now) {
			record.Status = "failed"
			record.Tracked = false
			if record.CompletedAt == "" {
				record.CompletedAt = now.Format(time.RFC3339)
			}
			if record.Error == "" {
				record.Error = monitorStaleUntrackedRunError
			}
			byRunID[runID] = record
		}
		if isTerminalMonitorRunStatus(record.Status) {
			h.clearMonitorRunState(runID)
		}
	}
}

func (h *Handler) listMonitorStoreRuns(ctx context.Context) []httpchannel.Run {
	if h == nil || h.store == nil {
		return nil
	}
	runs, err := h.store.List(ctx)
	if err != nil {
		return nil
	}
	return runs
}

func (h *Handler) monitorRunStatusFromStore(ctx context.Context, runID string) (string, bool) {
	if h == nil || h.store == nil {
		return "", false
	}
	run, err := h.store.Get(ctx, runID)
	if err != nil {
		return "", false
	}
	status := normalizeMonitorRunStatus(run.Status)
	if status == "" {
		return "", false
	}
	return status, true
}

func mergeStoredRunIntoMonitorRecord(record monitorRunRecord, run httpchannel.Run) monitorRunRecord {
	if strings.TrimSpace(record.Source) == "" {
		record.Source = strings.TrimSpace(run.Source)
	}
	if strings.TrimSpace(record.Message) == "" {
		record.Message = strings.TrimSpace(run.Message)
	}
	if strings.TrimSpace(record.ModelProvider) == "" {
		record.ModelProvider = strings.TrimSpace(run.Provider)
	}
	if strings.TrimSpace(record.ModelName) == "" {
		record.ModelName = strings.TrimSpace(run.Model)
	}
	if strings.TrimSpace(record.SessionID) == "" {
		record.SessionID = strings.TrimSpace(run.SessionID)
	}
	if strings.TrimSpace(record.StartedAt) == "" && !run.CreatedAt.IsZero() {
		record.StartedAt = run.CreatedAt.UTC().Format(time.RFC3339)
	}

	storeStatus := normalizeMonitorRunStatus(run.Status)
	if isTerminalMonitorRunStatus(storeStatus) {
		record.Status = storeStatus
		record.Tracked = false
		if strings.TrimSpace(record.Error) == "" {
			record.Error = strings.TrimSpace(run.Error)
		}
		if strings.TrimSpace(record.ArtifactPath) == "" {
			record.ArtifactPath = strings.TrimSpace(run.ArtifactPath)
		}
		if strings.TrimSpace(record.CompletedAt) == "" && !run.UpdatedAt.IsZero() {
			record.CompletedAt = run.UpdatedAt.UTC().Format(time.RFC3339)
		}
	}

	return record
}

func monitorRecordFromStoredRun(run httpchannel.Run) monitorRunRecord {
	record := monitorRunRecord{
		RunID:         strings.TrimSpace(run.ID),
		AgentID:       strings.TrimSpace(run.AgentID),
		Source:        strings.TrimSpace(run.Source),
		Role:          classifyMonitorRunRole(run.Source),
		Message:       strings.TrimSpace(run.Message),
		ModelProvider: strings.TrimSpace(run.Provider),
		ModelName:     strings.TrimSpace(run.Model),
		SessionID:     strings.TrimSpace(run.SessionID),
		Status:        normalizeMonitorRunStatus(run.Status),
		Error:         strings.TrimSpace(run.Error),
		ArtifactPath:  strings.TrimSpace(run.ArtifactPath),
	}
	if record.Role == "" {
		record.Role = "main"
	}
	if record.Status == "" {
		record.Status = "running"
	}
	if !run.CreatedAt.IsZero() {
		record.StartedAt = run.CreatedAt.UTC().Format(time.RFC3339)
	}
	if isTerminalMonitorRunStatus(record.Status) && !run.UpdatedAt.IsZero() {
		record.CompletedAt = run.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return record
}

func normalizeMonitorRunStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "cancelled":
		return "canceled"
	default:
		return normalized
	}
}

func isTerminalMonitorRunStatus(status string) bool {
	switch normalizeMonitorRunStatus(status) {
	case "completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func shouldRetireStaleUntrackedMonitorRun(record monitorRunRecord, storedByID map[string]httpchannel.Run, now time.Time) bool {
	if normalizeMonitorRunStatus(record.Status) != "running" {
		return false
	}
	if record.Tracked {
		return false
	}
	runID := strings.TrimSpace(record.RunID)
	if runID == "" {
		return false
	}
	if _, ok := storedByID[runID]; ok {
		return false
	}
	startedAt, ok := parseMonitorRunTimestamp(record.StartedAt)
	if !ok {
		return false
	}
	if now.Before(startedAt) {
		return false
	}
	return now.Sub(startedAt) > monitorStaleUntrackedRunTTL
}

func parseMonitorRunTimestamp(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func isCanceledMonitorError(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "context canceled") || strings.Contains(normalized, "context cancelled") {
		return true
	}
	if strings.Contains(normalized, "run canceled") || strings.Contains(normalized, "run cancelled") {
		return true
	}
	if strings.Contains(normalized, "cancelled") || strings.Contains(normalized, "canceled") {
		return true
	}
	return false
}

func (h *Handler) noteMonitorRunState(runID, status string) {
	if h == nil {
		return
	}
	runID = strings.TrimSpace(runID)
	status = normalizeMonitorRunStatus(status)
	if runID == "" || status == "" {
		return
	}
	h.monitorRunMu.Lock()
	defer h.monitorRunMu.Unlock()
	if h.monitorRuns == nil {
		h.monitorRuns = make(map[string]monitorRunState)
	}
	h.monitorRuns[runID] = monitorRunState{Status: status, UpdatedAt: time.Now().UTC()}
}

func (h *Handler) clearMonitorRunState(runID string) {
	if h == nil {
		return
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	h.monitorRunMu.Lock()
	defer h.monitorRunMu.Unlock()
	delete(h.monitorRuns, runID)
}

func (h *Handler) snapshotMonitorRunStates(now time.Time) map[string]monitorRunState {
	if h == nil {
		return nil
	}
	h.monitorRunMu.Lock()
	defer h.monitorRunMu.Unlock()
	if len(h.monitorRuns) == 0 {
		return nil
	}
	out := make(map[string]monitorRunState, len(h.monitorRuns))
	for runID, state := range h.monitorRuns {
		if now.Sub(state.UpdatedAt) > monitorRunStateTTL {
			delete(h.monitorRuns, runID)
			continue
		}
		out[runID] = state
	}
	return out
}

func firstStringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if values == nil {
			return ""
		}
		raw, ok := values[key]
		if !ok || raw == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(raw))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func classifyMonitorRunRole(source string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), "subagent/") {
		return "subagent"
	}
	return "main"
}

func firstMonitorTimestamp(record monitorRunRecord) string {
	if strings.TrimSpace(record.StartedAt) != "" {
		return record.StartedAt
	}
	return record.CompletedAt
}

func (h *Handler) handleAgentDocs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getAgentDocs(w, r)
	case http.MethodPost:
		h.setAgentDoc(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) getAgentDocs(w http.ResponseWriter, r *http.Request) {
	agentID, err := normalizeDashboardAgentID(strings.TrimSpace(r.URL.Query().Get("agent_id")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	docs := make([]agentDocPayload, 0, len(dashboardEditableDocNames))
	for _, name := range dashboardEditableDocNames {
		doc, readErr := h.readAgentDoc(agentID, name)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusInternalServerError)
			return
		}
		docs = append(docs, doc)
	}

	writeJSON(w, map[string]any{
		"agent_id":         agentID,
		"available_agents": h.listDashboardAgentIDs(),
		"documents":        docs,
	})
}

func (h *Handler) setAgentDoc(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID string `json:"agent_id"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	agentID, err := normalizeDashboardAgentID(req.AgentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	displayName, resolvedName, aliasFor, ok := resolveDashboardDocNames(req.Name)
	if !ok {
		http.Error(w, "unsupported document name", http.StatusBadRequest)
		return
	}

	agentDir := filepath.Join(h.rootDir, ".openclawssy", "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		http.Error(w, "failed to create agent docs directory", http.StatusInternalServerError)
		return
	}

	docPath := filepath.Join(agentDir, resolvedName)
	if err := os.WriteFile(docPath, []byte(req.Content), 0o600); err != nil {
		http.Error(w, "failed to save document", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"ok":            true,
		"agent_id":      agentID,
		"name":          displayName,
		"resolved_name": resolvedName,
		"alias_for":     aliasFor,
		"stored_bytes":  len(req.Content),
	})
}

func (h *Handler) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getSkills(w, r)
	case http.MethodPost:
		h.postSkills(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) getSkills(w http.ResponseWriter, r *http.Request) {
	agentID, err := normalizeDashboardAgentID(strings.TrimSpace(r.URL.Query().Get("agent_id")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	workspaceRoot := h.dashboardWorkspaceRoot()
	installed, err := listInstalledSkills(workspaceRoot)
	if err != nil {
		http.Error(w, "failed to list skills", http.StatusInternalServerError)
		return
	}
	activated, err := h.readActivatedSkills(agentID)
	if err != nil {
		http.Error(w, "failed to read agent skill activation", http.StatusInternalServerError)
		return
	}

	installableNames := make([]string, 0, len(builtInSkillCatalog))
	for name := range builtInSkillCatalog {
		installableNames = append(installableNames, name)
	}
	sort.Strings(installableNames)
	installable := make([]map[string]any, 0, len(installableNames))
	for _, name := range installableNames {
		installable = append(installable, map[string]any{
			"name":      name,
			"installed": containsString(installed, name),
		})
	}

	writeJSON(w, map[string]any{
		"agent_id":         agentID,
		"available_agents": h.listDashboardAgentIDs(),
		"installable":      installable,
		"installed_skills": installed,
		"activated_skills": activated,
	})
}

func (h *Handler) postSkills(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action  string `json:"action"`
		Name    string `json:"name"`
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	name := normalizeSkillName(req.Name)
	if name == "" {
		http.Error(w, "skill name is required", http.StatusBadRequest)
		return
	}
	workspaceRoot := h.dashboardWorkspaceRoot()

	switch action {
	case "install":
		if err := installBuiltInSkill(workspaceRoot, name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case "activate", "deactivate":
		agentID, err := normalizeDashboardAgentID(req.AgentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		installed, err := listInstalledSkills(workspaceRoot)
		if err != nil {
			http.Error(w, "failed to list skills", http.StatusInternalServerError)
			return
		}
		if !containsString(installed, name) {
			http.Error(w, fmt.Sprintf("skill %s is not installed", name), http.StatusBadRequest)
			return
		}
		if err := h.setSkillActivation(agentID, name, action == "activate"); err != nil {
			http.Error(w, "failed to update agent skill activation", http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "action must be install, activate, or deactivate", http.StatusBadRequest)
		return
	}

	h.getSkills(w, httptestCloneRequestWithAgent(r, req.AgentID))
}

func (h *Handler) handleWorkspaceEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceRoot, absPath, relPath, err := h.resolveDashboardWorkspacePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "workspace path not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read workspace path", http.StatusInternalServerError)
		return
	}
	payload := make([]workspaceEntryPayload, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		itemRel := entry.Name()
		if relPath != "." {
			itemRel = filepath.Join(relPath, entry.Name())
		}
		item := workspaceEntryPayload{
			Name:       entry.Name(),
			Path:       filepath.ToSlash(itemRel),
			Kind:       "file",
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
			MIMEType:   mime.TypeByExtension(strings.ToLower(filepath.Ext(entry.Name()))),
		}
		if entry.IsDir() {
			item.Kind = "dir"
			item.SizeBytes = 0
			item.MIMEType = ""
		}
		payload = append(payload, item)
	}
	sort.Slice(payload, func(i, j int) bool {
		if payload[i].Kind != payload[j].Kind {
			return payload[i].Kind == "dir"
		}
		return strings.ToLower(payload[i].Name) < strings.ToLower(payload[j].Name)
	})
	parentPath := ""
	if relPath != "." {
		parentPath = filepath.ToSlash(filepath.Dir(relPath))
		if parentPath == "." {
			parentPath = ""
		}
	}
	writeJSON(w, map[string]any{
		"workspace_root": filepath.ToSlash(workspaceRoot),
		"path":           filepath.ToSlash(relPath),
		"parent_path":    parentPath,
		"entries":        payload,
	})
}

func (h *Handler) handleWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceRoot, absPath, relPath, err := h.resolveDashboardWorkspacePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "workspace file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to stat workspace file", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "workspace path is a directory", http.StatusBadRequest)
		return
	}
	const previewLimitBytes = 256 * 1024
	file, err := os.Open(absPath)
	if err != nil {
		http.Error(w, "failed to open workspace file", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, previewLimitBytes+1))
	if err != nil {
		http.Error(w, "failed to read workspace file", http.StatusInternalServerError)
		return
	}
	truncated := len(data) > previewLimitBytes
	if truncated {
		data = data[:previewLimitBytes]
	}
	isText := utf8.Valid(data) && !strings.ContainsRune(string(data), '\x00')
	content := ""
	previewNotice := ""
	if isText {
		content = string(data)
		if truncated {
			previewNotice = fmt.Sprintf("Preview truncated to %d bytes.", previewLimitBytes)
		}
	} else {
		previewNotice = "Binary or non-UTF-8 file preview is not shown in the dashboard."
	}
	writeJSON(w, map[string]any{
		"workspace_root": filepath.ToSlash(workspaceRoot),
		"path":           filepath.ToSlash(relPath),
		"name":           info.Name(),
		"size_bytes":     info.Size(),
		"modified_at":    info.ModTime().UTC().Format(time.RFC3339),
		"mime_type":      mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name()))),
		"is_text":        isText,
		"truncated":      truncated,
		"preview_notice": previewNotice,
		"content":        content,
	})
}

func (h *Handler) resolveDashboardWorkspacePath(rawPath string) (string, string, string, error) {
	workspaceRoot := filepath.Clean(h.dashboardWorkspaceRoot())
	requested := strings.TrimSpace(rawPath)
	if requested == "" || requested == "/" {
		return workspaceRoot, workspaceRoot, ".", nil
	}
	var absPath string
	var relPath string
	if filepath.IsAbs(requested) {
		absPath = filepath.Clean(requested)
		rel, err := filepath.Rel(workspaceRoot, absPath)
		if err != nil {
			return "", "", "", errors.New("invalid workspace path")
		}
		relPath = rel
	} else {
		relPath = filepath.Clean(strings.TrimPrefix(filepath.ToSlash(requested), "/"))
		absPath = filepath.Join(workspaceRoot, relPath)
	}
	if relPath == "." {
		return workspaceRoot, workspaceRoot, ".", nil
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) || strings.HasPrefix(relPath, "../") {
		return "", "", "", errors.New("workspace path escapes root")
	}
	absPath = filepath.Clean(absPath)
	if absPath != workspaceRoot && !strings.HasPrefix(absPath, workspaceRoot+string(os.PathSeparator)) {
		return "", "", "", errors.New("workspace path escapes root")
	}
	return workspaceRoot, absPath, relPath, nil
}

func (h *Handler) dashboardWorkspaceRoot() string {
	defaultRoot := filepath.Join(h.rootDir, "workspace")
	cfgPath := filepath.Join(h.rootDir, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		return defaultRoot
	}
	root := strings.TrimSpace(cfg.Workspace.Root)
	if root == "" {
		return defaultRoot
	}
	resolved := root
	if filepath.IsAbs(root) {
		resolved = root
	} else {
		resolved = filepath.Clean(filepath.Join(h.rootDir, root))
	}
	if directoryExists(resolved) {
		return resolved
	}
	for _, fallback := range []string{defaultRoot, filepath.Join(h.rootDir, filepath.Base(resolved))} {
		if directoryExists(fallback) {
			return filepath.Clean(fallback)
		}
	}
	return filepath.Clean(resolved)
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func listInstalledSkills(workspaceRoot string) ([]string, error) {
	skillsDir := filepath.Join(workspaceRoot, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	installed := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		name := normalizeSkillName(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if name != "" {
			installed = append(installed, name)
		}
	}
	sort.Strings(installed)
	return installed, nil
}

func installBuiltInSkill(workspaceRoot, name string) error {
	body, ok := builtInSkillCatalog[name]
	if !ok {
		return fmt.Errorf("skill %s is not installable", name)
	}
	skillsDir := filepath.Join(workspaceRoot, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(skillsDir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

func normalizeSkillName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return ""
	}
	for _, r := range name {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return name
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}

func (h *Handler) toolsDocPath(agentID string) string {
	return filepath.Join(h.rootDir, ".openclawssy", "agents", agentID, "TOOLS.md")
}

func (h *Handler) readActivatedSkills(agentID string) ([]string, error) {
	raw, err := os.ReadFile(h.toolsDocPath(agentID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	return parseActivatedSkillsBlock(string(raw)), nil
}

func (h *Handler) setSkillActivation(agentID, name string, enabled bool) error {
	toolsPath := h.toolsDocPath(agentID)
	raw, err := os.ReadFile(toolsPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		raw = []byte("# TOOLS\n\nEnabled core tools: skill.list, skill.read.\n")
	}
	next := updateActivatedSkillsBlock(string(raw), name, enabled)
	if err := os.MkdirAll(filepath.Dir(toolsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(toolsPath, []byte(next), 0o600)
}

func parseActivatedSkillsBlock(content string) []string {
	start := strings.Index(content, skillsBlockStartTag)
	if start < 0 {
		return []string{}
	}
	bodyStart := start + len(skillsBlockStartTag)
	endRel := strings.Index(content[bodyStart:], skillsBlockEndTag)
	if endRel < 0 {
		return []string{}
	}
	body := content[bodyStart : bodyStart+endRel]
	lines := strings.Split(body, "\n")
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if trimmed == "" {
			continue
		}
		name := normalizeSkillName(trimmed)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func renderActivatedSkillsBlock(skills []string) string {
	if len(skills) == 0 {
		return ""
	}
	lines := []string{skillsBlockStartTag, "## Activated Skills", "Add these skill names to your workflow with skill.read before execution."}
	for _, skill := range skills {
		lines = append(lines, "- "+skill)
	}
	lines = append(lines, skillsBlockEndTag)
	return "\n\n" + strings.Join(lines, "\n") + "\n"
}

func updateActivatedSkillsBlock(content, name string, enabled bool) string {
	current := parseActivatedSkillsBlock(content)
	nextSet := make(map[string]struct{}, len(current)+1)
	for _, skill := range current {
		nextSet[skill] = struct{}{}
	}
	if enabled {
		nextSet[name] = struct{}{}
	} else {
		delete(nextSet, name)
	}
	nextList := make([]string, 0, len(nextSet))
	for skill := range nextSet {
		nextList = append(nextList, skill)
	}
	sort.Strings(nextList)
	nextBlock := renderActivatedSkillsBlock(nextList)

	start := strings.Index(content, skillsBlockStartTag)
	if start < 0 {
		if nextBlock == "" {
			return content
		}
		return strings.TrimRight(content, "\n") + nextBlock
	}
	bodyStart := start + len(skillsBlockStartTag)
	endRel := strings.Index(content[bodyStart:], skillsBlockEndTag)
	if endRel < 0 {
		if nextBlock == "" {
			return content
		}
		return content + nextBlock
	}
	end := bodyStart + endRel + len(skillsBlockEndTag)
	if nextBlock == "" {
		return strings.TrimRight(content[:start]+content[end:], "\n") + "\n"
	}
	return content[:start] + nextBlock + content[end:]
}

func httptestCloneRequestWithAgent(r *http.Request, agentID string) *http.Request {
	next := r.Clone(r.Context())
	q := next.URL.Query()
	normalized, err := normalizeDashboardAgentID(agentID)
	if err != nil {
		normalized = "default"
	}
	q.Set("agent_id", normalized)
	next.URL.RawQuery = q.Encode()
	return next
}

func (h *Handler) readAgentDoc(agentID, name string) (agentDocPayload, error) {
	displayName, resolvedName, aliasFor, ok := resolveDashboardDocNames(name)
	if !ok {
		return agentDocPayload{}, errors.New("unsupported document name")
	}
	docPath := filepath.Join(h.rootDir, ".openclawssy", "agents", agentID, resolvedName)
	raw, err := os.ReadFile(docPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agentDocPayload{Name: displayName, ResolvedName: resolvedName, AliasFor: aliasFor, Exists: false}, nil
		}
		return agentDocPayload{}, errors.New("failed to read agent document")
	}
	return agentDocPayload{Name: displayName, ResolvedName: resolvedName, AliasFor: aliasFor, Content: string(raw), Exists: true}, nil
}

func (h *Handler) listDashboardAgentIDs() []string {
	ids := map[string]struct{}{"default": {}}

	cfgPath := filepath.Join(h.rootDir, ".openclawssy", "config.json")
	if cfg, err := config.LoadOrDefault(cfgPath); err == nil {
		for _, agentID := range cfg.Agents.EnabledAgentIDs {
			if normalized, err := normalizeDashboardAgentID(agentID); err == nil {
				ids[normalized] = struct{}{}
			}
		}
		for agentID := range cfg.Agents.Profiles {
			if normalized, err := normalizeDashboardAgentID(agentID); err == nil {
				ids[normalized] = struct{}{}
			}
		}
		for _, agentID := range []string{cfg.Chat.DefaultAgentID, cfg.Discord.DefaultAgentID, cfg.Telegram.DefaultAgentID} {
			if normalized, err := normalizeDashboardAgentID(agentID); err == nil {
				ids[normalized] = struct{}{}
			}
		}
	}

	agentsDir := filepath.Join(h.rootDir, ".openclawssy", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			id, err := normalizeDashboardAgentID(entry.Name())
			if err != nil {
				continue
			}
			ids[id] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return []string{"default"}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func resolveDashboardDocNames(raw string) (displayName string, resolvedName string, aliasFor string, ok bool) {
	name := strings.ToUpper(strings.TrimSpace(raw))
	name = strings.TrimSuffix(name, ".MD")
	switch name {
	case "SOUL":
		return "SOUL.md", "SOUL.md", "", true
	case "RULES":
		return "RULES.md", "RULES.md", "", true
	case "TOOLS":
		return "TOOLS.md", "TOOLS.md", "", true
	case "SPECPLAN":
		return "SPECPLAN.md", "SPECPLAN.md", "", true
	case "DEVPLAN":
		return "DEVPLAN.md", "DEVPLAN.md", "", true
	case "HANDOFF":
		return "HANDOFF.md", "HANDOFF.md", "", true
	case "HEARTBEAT":
		return "HEARTBEAT.md", "HANDOFF.md", "HANDOFF.md", true
	default:
		return "", "", "", false
	}
}

func normalizeDashboardAgentID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		id = "default"
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\\`) {
		return "", errors.New("invalid agent id")
	}
	for _, r := range id {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum || r == '-' || r == '_' {
			continue
		}
		return "", errors.New("invalid agent id")
	}
	return id, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func parseLimitOffset(q map[string][]string, defaultLimit, maxLimit int) (int, int, error) {
	limit := defaultLimit
	offset := 0
	if rawLimit := strings.TrimSpace(firstQueryValue(q, "limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > maxLimit {
			return 0, 0, errors.New("invalid limit")
		}
		limit = parsed
	}
	if rawOffset := strings.TrimSpace(firstQueryValue(q, "offset")); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			return 0, 0, errors.New("invalid offset")
		}
		offset = parsed
	}
	return limit, offset, nil
}

func firstQueryValue(q map[string][]string, key string) string {
	if len(q[key]) == 0 {
		return ""
	}
	return q[key][0]
}
