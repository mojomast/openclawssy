package dashboard

import (
	"bufio"
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
	"time"
	"unicode"
	"unicode/utf8"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
	"openclawssy/internal/runtime"
	"openclawssy/internal/scheduler"
	"openclawssy/internal/secrets"
	"openclawssy/internal/skillcatalog"
)

type Handler struct {
	rootDir        string
	store          httpchannel.RunStore
	schedulerStore *scheduler.Store
	runCanceller   dashboardRunCanceller
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

//go:embed ui/dist ui/index.html ui/app.js ui/styles.css ui/help ui/src
var dashboardUIFS embed.FS

func New(rootDir string, store httpchannel.RunStore, schedulerStore ...*scheduler.Store) *Handler {
	var opts Options
	if len(schedulerStore) > 0 {
		opts.SchedulerStore = schedulerStore[0]
	}
	return NewWithOptions(rootDir, store, opts)
}

func NewWithOptions(rootDir string, store httpchannel.RunStore, opts Options) *Handler {
	return &Handler{rootDir: rootDir, store: store, schedulerStore: opts.SchedulerStore, runCanceller: opts.RunCanceller}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/favicon.ico", h.serveDashboardFavicon)
	mux.HandleFunc("/dashboard", h.serveDashboard)
	mux.HandleFunc("/dashboard/", h.serveDashboard)
	mux.HandleFunc("/dashboard-legacy", h.serveLegacyDashboard)
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
	mux.HandleFunc("/api/admin/agents", h.handleAgents)
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
	// Serve React build from dist/ if available, fallback to legacy index.html
	content, err := dashboardUIFS.ReadFile("ui/dist/index.html")
	if err != nil {
		// Fallback to original index.html for development
		content, err = dashboardUIFS.ReadFile("ui/index.html")
		if err != nil {
			http.Error(w, "dashboard ui not available", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Prevent caching of index.html to ensure fresh SPA on reload
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(content)
}

func (h *Handler) serveLegacyDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
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

	// Try to serve from dist/ first (React build), then fallback to ui/ (legacy)
	content, err := dashboardUIFS.ReadFile("ui/dist/" + assetPath)
	if err != nil {
		content, err = dashboardUIFS.ReadFile("ui/" + assetPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
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
	validDelegation := map[string]bool{"": true, "prompt_only": true, "tool_gated": true, "auto_execute": true}
	if !validDelegation[strings.TrimSpace(cfg.Agents.SubAgentDefaults.DelegationMode)] {
		set("agents.subagent_defaults.delegation_mode", "Delegation mode must be one of prompt_only, tool_gated, auto_execute.")
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
	runs, err := h.collectMonitorRuns(limit, agentFilter)
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
	if err := h.runCanceller.Cancel(runID); err != nil {
		writeJSON(w, map[string]any{"run_id": runID, "cancelled": false, "tracked": false})
		return
	}
	writeJSON(w, map[string]any{"run_id": runID, "cancelled": true, "tracked": true})
}

func (h *Handler) collectMonitorRuns(limit int, agentFilter string) ([]monitorRunRecord, error) {
	agentsDir := filepath.Join(h.rootDir, ".openclawssy", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
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
			return nil, err
		}
	}
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
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		var event struct {
			Timestamp time.Time      `json:"ts"`
			Type      string         `json:"type"`
			RunID     string         `json:"run_id"`
			AgentID   string         `json:"agent_id"`
			Payload   map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		runID := strings.TrimSpace(event.RunID)
		agentID := strings.TrimSpace(event.AgentID)
		if runID == "" || agentID == "" {
			continue
		}
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
				record.Status = "failed"
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
	return scanner.Err()
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

const dashboardHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Openclawssy Dashboard</title>
<style>
*{box-sizing:border-box}
body{font-family:ui-monospace,Menlo,monospace;background:#0b1020;color:#e8eefc;margin:0;padding:0;height:100vh;display:flex;flex-direction:column}
.header{background:#151b2e;padding:10px 20px;border-bottom:1px solid #2a3447;display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap}
.header h1{margin:0;font-size:1.2em}
.main-content{flex:1;display:flex;flex-direction:column;overflow:hidden}
.chat-section{flex:1;display:flex;flex-direction:column;padding:20px;min-height:0}
.chat-container{flex:0 0 auto;height:42vh;min-height:220px;max-height:80vh;border:1px solid #333;padding:15px;overflow-y:auto;background:#0d1325;border-radius:8px;margin-bottom:10px;scroll-behavior:smooth;resize:vertical}
.chat-message{margin:8px 0;padding:12px;border-radius:8px;max-width:85%;word-wrap:break-word}
.chat-user{background:#1a3a5c;margin-left:auto;margin-right:0}
.chat-assistant{background:#1a2e1a;margin-left:0;margin-right:auto;border-left:3px solid #4a9}
.chat-assistant pre{background:#0a150a;padding:10px;border-radius:4px;overflow-x:auto;margin:8px 0}
.chat-assistant code{background:#1a2e1a;padding:2px 6px;border-radius:3px;font-size:0.9em}
.chat-input{display:flex;gap:10px;padding:0}
.chat-input input{flex:1;padding:12px;border:1px solid #333;background:#151b2e;color:#e8eefc;border-radius:6px;font-size:1em}
.chat-input button{padding:12px 24px;background:#2a5;border:none;color:#fff;border-radius:6px;cursor:pointer;font-weight:bold}
.chat-input button:hover{background:#3b6}
.chat-controls{display:flex;gap:8px;flex-wrap:wrap;margin:0 0 10px 0}
.chat-controls button{padding:8px 12px;background:#355d7f;color:#fff;border:none;border-radius:6px;cursor:pointer}
.chat-controls input{padding:8px 10px;border:1px solid #333;background:#151b2e;color:#e8eefc;border-radius:6px;min-width:250px}
.chat-size-control{display:flex;align-items:center;gap:8px;padding:6px 10px;border:1px solid #2b3a52;background:#10182b;border-radius:6px;font-size:0.8em;color:#aac3de}
.chat-size-control input{min-width:140px;padding:0;border:none;background:transparent}
.chat-size-value{min-width:40px;text-align:right;color:#dbe7f4}
.tool-pane{border:1px solid #2a3447;background:#111a2e;border-radius:8px;padding:10px;margin:0 0 10px 0;max-height:160px;overflow-y:auto}
.tool-pane h4{margin:0;color:#8fb8dc;font-size:0.85em}
.tool-item{font-size:0.8em;line-height:1.4;padding:6px 0;border-top:1px solid #22324a}
.tool-item:first-of-type{border-top:none;padding-top:0}
.tool-empty{color:#7187a0;font-size:0.8em}
.tool-preview{color:#d6e2f0}
.tool-full{margin-top:6px;padding:8px;background:#0c1426;border:1px solid #1f3048;border-radius:4px;max-height:220px;overflow:auto;white-space:pre-wrap;word-break:break-word}
.session-pane{border:1px solid #2a3447;background:#111a2e;border-radius:8px;padding:10px;margin:0 0 10px 0;max-height:180px;overflow-y:auto}
.session-pane h4{margin:0;color:#8fb8dc;font-size:0.85em}
.session-item{display:flex;justify-content:space-between;align-items:center;gap:8px;font-size:0.8em;line-height:1.3;padding:6px 0;border-top:1px solid #22324a}
.session-item:first-of-type{border-top:none;padding-top:0}
.session-meta{color:#90a7c0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.session-actions button{padding:4px 8px;background:#2f5476;border:none;color:#e8eefc;border-radius:4px;cursor:pointer}
.session-actions button:hover{background:#3a658e}
.pane-header{display:flex;justify-content:space-between;align-items:center;gap:8px;margin-bottom:8px}
.pane-controls{display:flex;align-items:center;gap:6px;flex:1;justify-content:flex-end;flex-wrap:wrap}
.pane-search{min-width:150px;max-width:240px;flex:1}
.pane-search input{width:100%;padding:6px 8px;border:1px solid #2c3b55;background:#0b1020;color:#dbe7f4;border-radius:4px;font-size:0.78em}
.pane-select select{padding:6px 8px;border:1px solid #2c3b55;background:#0b1020;color:#dbe7f4;border-radius:4px;font-size:0.76em}
.pane-toggle{padding:5px 10px;background:#2b4763;color:#e8eefc;border:none;border-radius:4px;cursor:pointer;font-size:0.74em;line-height:1}
.pane-toggle:hover{background:#3a5f81}
.collapsible-body{display:block}
.is-collapsed .collapsible-body{display:none}
.is-collapsed{max-height:none;overflow:hidden}
.session-active-tag{display:inline-block;margin-left:6px;padding:2px 6px;background:#204966;border:1px solid #356a91;border-radius:10px;color:#d9ecff;font-size:0.72em}
.status-section{background:#151b2e;padding:15px 20px;border-top:1px solid #2a3447;max-height:200px;overflow-y:auto}
.status-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
.status-header h3{margin:0;font-size:0.9em;color:#8a9}
.status-actions{display:flex;gap:8px;align-items:center}
.status-content{font-size:0.85em;background:#0b1020;padding:10px;border-radius:4px;max-height:120px;overflow-y:auto}
.admin-section{background:#0d1325;padding:20px;border-top:1px solid #2a3447;max-height:420px;overflow-y:auto}
.admin-header{display:flex;justify-content:space-between;align-items:center;margin:0 0 12px 0}
.admin-header h3{margin:0;font-size:0.95em;color:#9db2d4}
.admin-tabs{display:flex;gap:10px;margin-bottom:15px}
.admin-tab{padding:8px 16px;background:#151b2e;border:1px solid #333;border-radius:4px;cursor:pointer;color:#8a9}
.admin-tab.active{background:#1a3a5c;color:#e8eefc}
.admin-panel{display:none}
.admin-panel.active{display:block}
.section-title{color:#9db2d4;margin:14px 0 8px 0;font-size:0.9em;font-weight:bold}
.form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
.field{display:flex;flex-direction:column;gap:4px}
.field label{font-size:0.8em;color:#8a9}
.field input,.field select,.field textarea{width:100%;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px;font-family:inherit}
.field textarea{min-height:72px;resize:vertical}
.field.full{grid-column:1 / -1}
.toggles{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px 14px;margin:8px 0}
.toggle{display:flex;align-items:center;gap:8px;font-size:0.85em;color:#d1d9eb}
.toggle input{margin:0}
.button-row{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:10px}
button{background:#2a4a6c;border:none;color:#e8eefc;padding:8px 16px;border-radius:4px;cursor:pointer}
button:hover{background:#3a5a7c}
button.secondary{background:#254054}
button.secondary:hover{background:#2d4c61}
.msg{min-height:18px;font-size:0.82em;margin:0 0 8px 0}
.msg.error{color:#ff8f8f}
.msg.success{color:#8ff0c1}
pre{background:#0b1020;padding:10px;border-radius:4px;overflow-x:auto;font-size:0.85em}
.helper-box{padding:10px;border:1px solid #2a3447;border-radius:6px;background:#10182b;margin-bottom:10px}
.helper-label{font-size:0.8em;color:#8a9;margin-bottom:4px}
details{margin-top:8px}
summary{cursor:pointer;color:#9db2d4}
@media (max-width:900px){
  .chat-container{height:45vh;max-height:70vh}
  .admin-section{max-height:none}
  .form-grid,.toggles{grid-template-columns:1fr}
  .chat-input{flex-wrap:wrap}
  .chat-input button{padding:10px 14px}
  .chat-size-control{width:100%}
}
</style>
</head><body>
<div class="header">
<h1>Openclawssy Dashboard</h1>
<span style="color:#8a9;font-size:0.8em">Model: <span id="modelInfo">Loading...</span></span>
</div>

<div class="main-content">
<div class="chat-section">
<div class="chat-container" id="chatHistory"></div>
<div class="tool-pane" id="toolPane"><div class="pane-header"><h4>Tool Activity</h4><button class="pane-toggle" id="toggleToolPane" onclick="toggleSection('toolPane')">Collapse</button></div><div class="collapsible-body"><div class="pane-controls" style="margin-bottom:8px"><div class="pane-select"><select id="toolStatusFilter" onchange="renderToolActivity()"><option value="all">All</option><option value="errors">Errors</option><option value="output">Output</option></select></div><div class="pane-search"><input id="toolFilter" placeholder="Filter tool activity" oninput="renderToolActivity()"/></div></div><div id="toolHistory" class="tool-empty">No tool activity yet.</div></div></div>
<div class="session-pane" id="sessionPane"><div class="pane-header"><h4>Recent Sessions</h4><button class="pane-toggle" id="toggleSessionPane" onclick="toggleSection('sessionPane')">Collapse</button></div><div class="collapsible-body"><div class="pane-controls" style="margin-bottom:8px"><div class="pane-select"><select id="sessionSortMode" onchange="renderSessionList()"><option value="recent">Recent</option><option value="oldest">Oldest</option><option value="active">Active First</option></select></div><div class="pane-search"><input id="sessionFilter" placeholder="Filter sessions" oninput="renderSessionList()"/></div></div><div id="sessionList" class="tool-empty">No sessions yet.</div></div></div>
<div class="chat-controls">
<button onclick="startNewChat()">New chat</button>
<button onclick="listChats()">List chats</button>
<button onclick="refreshSessions()">Refresh sessions</button>
<button onclick="focusChat()">Focus chat</button>
<button onclick="resetLayout()">Reset layout</button>
<label class="chat-size-control">Chat height<input id="chatHeightRange" type="range" min="220" max="1000" step="20" oninput="setChatHeight(this.value,true)"/><span id="chatHeightValue" class="chat-size-value">0px</span></label>
<input id="resumeSessionID" placeholder="session id for /resume"/>
<button onclick="resumeChat()">Resume</button>
</div>
<div class="chat-input">
<input id="chatMessage" placeholder="Type your message and press Enter..." onkeypress="if(event.key==='Enter')sendChat()"/>
<button onclick="sendChat()">Send</button>
</div>
</div>

<div class="status-section" id="statusSection">
<div class="status-header">
<h3>Status & Recent Runs</h3>
<div class="status-actions"><button onclick="loadStatus()" style="padding:4px 12px;font-size:0.8em">Refresh</button><button class="pane-toggle" id="toggleStatusSection" onclick="toggleSection('statusSection')">Collapse</button></div>
</div>
<div class="collapsible-body">
<div class="status-content" id="status">Loading...</div>
</div>
</div>

<div class="admin-section" id="adminSection">
<div class="admin-header"><h3>Admin Controls</h3><button class="pane-toggle" id="toggleAdminSection" onclick="toggleSection('adminSection')">Collapse</button></div>
<div class="collapsible-body">
<div class="admin-tabs">
<div class="admin-tab active" onclick="showTab(event,'config')">Config</div>
<div class="admin-tab" onclick="showTab(event,'secrets')">Secrets</div>
</div>
<div class="admin-panel active" id="config-panel">
<div class="button-row">
<button onclick="loadConfig()">Load</button>
<button onclick="saveConfig()">Save</button>
</div>
<div class="msg" id="cfgMessage"></div>

<div class="form-grid">
<div class="field">
<label for="cfgProvider">Provider</label>
<select id="cfgProvider" onchange="onProviderChange()">
<option value="openai">openai</option>
<option value="openrouter">openrouter</option>
<option value="requesty">requesty</option>
<option value="hatz">hatz</option>
<option value="zai">zai</option>
<option value="generic">generic</option>
</select>
</div>
<div class="field">
<label for="cfgModelName">Model name</label>
<input id="cfgModelName" type="text" placeholder="gpt-4.1-mini"/>
</div>
<div class="field">
<label for="cfgTemperature">Temperature</label>
<input id="cfgTemperature" type="number" step="0.1" placeholder="0.2"/>
</div>
<div class="field" id="genericBaseRow" style="display:none">
<label for="cfgGenericBaseURL">Generic base_url</label>
<input id="cfgGenericBaseURL" type="text" placeholder="https://example.com/v1"/>
</div>
</div>

<div class="section-title">Feature Toggles</div>
<div class="toggles">
<label class="toggle"><input id="cfgChatEnabled" type="checkbox"/>Chat enabled</label>
<label class="toggle"><input id="cfgDiscordEnabled" type="checkbox"/>Discord enabled</label>
<label class="toggle"><input id="cfgTelegramEnabled" type="checkbox"/>Telegram enabled</label>
<label class="toggle"><input id="cfgSandboxActive" type="checkbox" onchange="onSandboxActiveChange()"/>Sandbox active</label>
<label class="toggle"><input id="cfgShellExecEnabled" type="checkbox"/>Shell exec enabled</label>
</div>

<div class="form-grid">
<div class="field">
<label for="cfgSandboxProvider">Sandbox provider</label>
<select id="cfgSandboxProvider" onchange="updateRawPreview()">
<option value="docker">docker (recommended)</option>
<option value="local">local</option>
<option value="none">none</option>
</select>
</div>
<div class="field full">
<label>Sandbox notes</label>
<div style="font-size:0.85em;color:#9db2d4;line-height:1.4">
Use <code>docker</code> for isolated agent workspaces in separate containers (recommended).<br/>
Use <code>local</code> for shell access directly in this process environment.<br/>
Use <code>none</code> to disable sandbox execution tools.
</div>
</div>
</div>

<div class="section-title">Allowlists</div>
<div class="form-grid">
<div class="field">
<label for="cfgChatUsers">Chat senders</label>
<textarea id="cfgChatUsers" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgChatRooms">Chat rooms</label>
<textarea id="cfgChatRooms" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgDiscordUsers">Discord users</label>
<textarea id="cfgDiscordUsers" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgDiscordChannels">Discord channels</label>
<textarea id="cfgDiscordChannels" placeholder="one entry per line"></textarea>
</div>
<div class="field full">
<label for="cfgDiscordGuilds">Discord guilds</label>
<textarea id="cfgDiscordGuilds" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgTelegramUsers">Telegram users</label>
<textarea id="cfgTelegramUsers" placeholder="one entry per line"></textarea>
</div>
<div class="field">
<label for="cfgTelegramChats">Telegram chats</label>
<textarea id="cfgTelegramChats" placeholder="one entry per line"></textarea>
</div>
</div>

<details>
<summary>Raw JSON preview</summary>
<pre id="cfgRawPreview"></pre>
</details>
</div>

<div class="admin-panel" id="secrets-panel">
<div class="helper-box">
<div class="helper-label">Provider key helper</div>
<div class="field full">
<label for="providerSecretName">Recommended key name</label>
<input id="providerSecretName" type="text" readonly/>
</div>
<div class="button-row">
<input id="providerSecretValue" type="password" placeholder="provider API key" style="flex:1;min-width:230px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="storeProviderSecret()">Store provider key</button>
</div>
</div>

<div class="helper-box">
<div class="helper-label">Discord helper</div>
<div class="button-row">
<input id="discordTokenValue" type="password" placeholder="discord bot token" style="flex:1;min-width:230px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="storeDiscordToken()">Store discord bot token</button>
</div>
</div>

<div class="helper-box">
<div class="helper-label">Telegram helper</div>
<div class="button-row">
<input id="telegramTokenValue" type="password" placeholder="telegram bot token" style="flex:1;min-width:230px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="storeTelegramToken()">Store telegram bot token</button>
</div>
</div>

<div class="button-row">
<input id="sname" placeholder="provider/zai/api_key" style="width:300px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<input id="svalue" placeholder="secret value" type="password" style="width:300px;background:#0b1020;color:#e8eefc;border:1px solid #333;padding:8px;border-radius:4px"/>
<button onclick="setSecret()">Store Secret</button>
<button class="secondary" onclick="listSecrets()">List Keys</button>
</div>
<pre id="secrets"></pre>
</div>
</div>
</div>
</div>

<script>
let chatMessages=[];
let toolActivity=[];
let knownSessions=[];
let currentActiveSessionID='';
let currentConfig=null;
let chatPinnedToBottom=true;
let chatScrollTop=0;
const layoutStoragePrefix='dashboard.layout.';

function byId(id){return document.getElementById(id);}

function setChatHeight(value,persist){
const chat=byId('chatHistory');
const slider=byId('chatHeightRange');
const label=byId('chatHeightValue');
if(!chat)return;
let px=Number(value);
if(Number.isNaN(px)||px<220)px=220;
if(px>1000)px=1000;
chat.style.height=px+'px';
if(slider&&Number(slider.value)!==px)slider.value=String(px);
if(label)label.textContent=px+'px';
if(persist)localStorage.setItem(layoutStoragePrefix+'chatHeight',String(px));
}

function setSectionCollapsed(sectionID,collapsed,persist){
const section=byId(sectionID);
if(!section)return;
section.classList.toggle('is-collapsed',!!collapsed);
const toggle=section.querySelector('.pane-toggle');
if(toggle)toggle.textContent=collapsed?'Expand':'Collapse';
if(persist)localStorage.setItem(layoutStoragePrefix+sectionID,collapsed?'1':'0');
}

function toggleSection(sectionID){
const section=byId(sectionID);
if(!section)return;
setSectionCollapsed(sectionID,!section.classList.contains('is-collapsed'),true);
}

function focusChat(){
setSectionCollapsed('toolPane',true,true);
setSectionCollapsed('sessionPane',true,true);
setSectionCollapsed('statusSection',true,true);
setSectionCollapsed('adminSection',true,true);
setChatHeight(Math.max(420,Math.floor(window.innerHeight*0.62)),true);
}

function resetLayout(){
['toolPane','sessionPane','statusSection','adminSection'].forEach(function(id){setSectionCollapsed(id,false,true);});
setChatHeight(Math.max(320,Math.floor(window.innerHeight*0.42)),true);
}

function applyLayoutPreferences(){
const savedHeight=Number(localStorage.getItem(layoutStoragePrefix+'chatHeight'));
if(savedHeight>0){
setChatHeight(savedHeight,false);
}else{
setChatHeight(Math.max(320,Math.floor(window.innerHeight*0.42)),false);
}
['toolPane','sessionPane','statusSection','adminSection'].forEach(function(id){
setSectionCollapsed(id,localStorage.getItem(layoutStoragePrefix+id)==='1',false);
});
}

function bindChatResizePersistence(){
const chat=byId('chatHistory');
const slider=byId('chatHeightRange');
const saveCurrentHeight=function(){
if(!chat)return;
const px=Math.round(chat.getBoundingClientRect().height);
setChatHeight(px,true);
};
if(chat){
chat.addEventListener('mouseup',saveCurrentHeight);
chat.addEventListener('touchend',saveCurrentHeight);
}
if(slider){
slider.addEventListener('change',function(){setChatHeight(slider.value,true);});
}
}

function showTab(evt,tab){
document.querySelectorAll('.admin-tab').forEach(function(t){t.classList.remove('active');});
document.querySelectorAll('.admin-panel').forEach(function(p){p.classList.remove('active');});
evt.target.classList.add('active');
byId(tab+'-panel').classList.add('active');
}

function getToken(){
const params=new URLSearchParams(window.location.search);
const urlToken=params.get('token');
if(urlToken){localStorage.setItem('token',urlToken);return urlToken;}
const stored=localStorage.getItem('token');
if(stored)return stored;
const prompted=prompt('Bearer token');
if(prompted)localStorage.setItem('token',prompted);
return prompted;
}

async function j(url,opts){
opts=opts||{};
const t=getToken();
if(!t){alert('Bearer token required');return {ok:false,error:'missing token'};}
opts.headers=Object.assign({'Authorization':'Bearer '+t,'Content-Type':'application/json'},opts.headers||{});
const r=await fetch(url,opts);
const txt=await r.text();
let parsed;
try{parsed=JSON.parse(txt);}catch(e){parsed={raw:txt};}
if(!r.ok){
const errBody=parsed&&parsed.error;
if(errBody&&typeof errBody==='object'){
const msg=typeof errBody.message==='string'?errBody.message.trim():'';
const code=typeof errBody.code==='string'?errBody.code.trim():'';
parsed.error=msg||code||JSON.stringify(errBody);
if(code)parsed.error_code=code;
if(typeof errBody.retry_after_seconds==='number')parsed.retry_after_seconds=errBody.retry_after_seconds;
}else if(!parsed.error){
parsed.error=parsed.raw||txt||('request failed with status '+r.status);
}
parsed.ok=false;
parsed.status=r.status;
return parsed;
}
if(parsed&&typeof parsed==='object'&&typeof parsed.ok==='undefined')parsed.ok=true;
return parsed;
}

function setCfgMessage(message,type){
const el=byId('cfgMessage');
el.textContent=message||'';
el.className='msg'+(type?' '+type:'');
}

function listToText(items){
if(!Array.isArray(items)||items.length===0)return '';
return items.join('\n');
}

function textToList(value){
if(!value)return [];
return value.split(/[\s,;]+/).map(function(v){return v.trim();}).filter(function(v){return v.length>0;});
}

function providerSecretKey(provider){
return 'provider/'+provider+'/api_key';
}

function updateProviderSecretName(){
const provider=byId('cfgProvider').value||'zai';
byId('providerSecretName').value=providerSecretKey(provider);
}

function onProviderChange(){
const provider=byId('cfgProvider').value;
byId('genericBaseRow').style.display=provider==='generic'?'flex':'none';
updateProviderSecretName();
updateRawPreview();
}

function onSandboxActiveChange(){
const isActive=!!byId('cfgSandboxActive').checked;
const providerInput=byId('cfgSandboxProvider');
if(!providerInput)return;
if(isActive&&providerInput.value==='none')providerInput.value='docker';
updateRawPreview();
}

function ensureConfigShape(cfg){
if(!cfg.model)cfg.model={};
if(!cfg.providers)cfg.providers={};
if(!cfg.providers.generic)cfg.providers.generic={};
if(!cfg.chat)cfg.chat={};
if(!cfg.discord)cfg.discord={};
if(!cfg.telegram)cfg.telegram={};
if(!cfg.sandbox)cfg.sandbox={};
if(!cfg.shell)cfg.shell={};
if(!cfg.sandbox.provider)cfg.sandbox.provider='none';
}

function populateForm(cfg){
ensureConfigShape(cfg);
byId('cfgProvider').value=cfg.model.provider||'zai';
byId('cfgModelName').value=cfg.model.name||'';
byId('cfgTemperature').value=(typeof cfg.model.temperature==='number')?String(cfg.model.temperature):'';
byId('cfgGenericBaseURL').value=(cfg.providers.generic&&cfg.providers.generic.base_url)||'';
byId('cfgChatEnabled').checked=!!cfg.chat.enabled;
byId('cfgDiscordEnabled').checked=!!cfg.discord.enabled;
byId('cfgTelegramEnabled').checked=!!cfg.telegram.enabled;
byId('cfgSandboxActive').checked=!!cfg.sandbox.active;
const sandboxProvider=(cfg.sandbox.provider||'none').toLowerCase();
if(sandboxProvider==='local'||sandboxProvider==='none'||sandboxProvider==='docker'){
byId('cfgSandboxProvider').value=sandboxProvider;
}else{
byId('cfgSandboxProvider').value='docker';
}
byId('cfgShellExecEnabled').checked=!!cfg.shell.enable_exec;
byId('cfgChatUsers').value=listToText(cfg.chat.allow_users);
byId('cfgChatRooms').value=listToText(cfg.chat.allow_rooms);
byId('cfgDiscordUsers').value=listToText(cfg.discord.allow_users);
byId('cfgDiscordChannels').value=listToText(cfg.discord.allow_channels);
byId('cfgDiscordGuilds').value=listToText(cfg.discord.allow_guilds);
byId('cfgTelegramUsers').value=listToText(cfg.telegram.allow_users);
byId('cfgTelegramChats').value=listToText(cfg.telegram.allow_chats);
onProviderChange();
}

function cloneConfig(cfg){
return JSON.parse(JSON.stringify(cfg||{}));
}

function collectConfigFromForm(){
const cfg=cloneConfig(currentConfig);
ensureConfigShape(cfg);
const provider=byId('cfgProvider').value.trim().toLowerCase();
const modelName=byId('cfgModelName').value.trim();
const tempRaw=byId('cfgTemperature').value.trim();
cfg.model.provider=provider;
cfg.model.name=modelName;
if(tempRaw===''){
delete cfg.model.temperature;
}else{
cfg.model.temperature=Number(tempRaw);
}
cfg.chat.enabled=byId('cfgChatEnabled').checked;
cfg.discord.enabled=byId('cfgDiscordEnabled').checked;
cfg.telegram.enabled=byId('cfgTelegramEnabled').checked;
cfg.sandbox.active=byId('cfgSandboxActive').checked;
cfg.sandbox.provider=byId('cfgSandboxProvider').value.trim().toLowerCase();
cfg.shell.enable_exec=byId('cfgShellExecEnabled').checked;
cfg.chat.allow_users=textToList(byId('cfgChatUsers').value);
cfg.chat.allow_rooms=textToList(byId('cfgChatRooms').value);
cfg.discord.allow_users=textToList(byId('cfgDiscordUsers').value);
cfg.discord.allow_channels=textToList(byId('cfgDiscordChannels').value);
cfg.discord.allow_guilds=textToList(byId('cfgDiscordGuilds').value);
cfg.telegram.allow_users=textToList(byId('cfgTelegramUsers').value);
cfg.telegram.allow_chats=textToList(byId('cfgTelegramChats').value);
if(provider==='generic'){
cfg.providers.generic.base_url=byId('cfgGenericBaseURL').value.trim();
}
return cfg;
}

function validateConfigForm(){
const provider=byId('cfgProvider').value.trim();
const modelName=byId('cfgModelName').value.trim();
const tempRaw=byId('cfgTemperature').value.trim();
if(!provider)return 'provider is required';
if(!modelName)return 'model name is required';
if(tempRaw!==''&&Number.isNaN(Number(tempRaw)))return 'temperature must be numeric';
const sandboxProvider=byId('cfgSandboxProvider').value.trim().toLowerCase();
if(sandboxProvider!=='local'&&sandboxProvider!=='none'&&sandboxProvider!=='docker')return 'sandbox provider must be local, docker, or none';
if(byId('cfgSandboxActive').checked&&sandboxProvider==='none')return 'sandbox provider must be local or docker when sandbox is active';
if(provider==='generic'&&!byId('cfgGenericBaseURL').value.trim())return 'generic base_url is required';
return '';
}

function updateRawPreview(){
try{byId('cfgRawPreview').textContent=JSON.stringify(collectConfigFromForm(),null,2);}catch(e){byId('cfgRawPreview').textContent='';}
}

async function loadStatus(){
const data=await j('/api/admin/status');
if(data.model){byId('modelInfo').textContent=data.model.provider+'/'+data.model.name;}
let html='Runs: '+(data.run_count||0);
if(data.runs&&data.runs.length>0){
html+='<br><br>Recent:<br>';
data.runs.slice(0,5).forEach(function(run){
const status=run.status==='completed'?'✓':run.status==='failed'?'✗':'⏳';
html+=status+' '+run.id+' ('+run.source+')<br>';
});
}
byId('status').innerHTML=html;
}

async function loadConfig(){
setCfgMessage('','');
const cfg=await j('/api/admin/config');
if(!cfg.ok){
setCfgMessage('Failed to load config: '+(cfg.error||'unknown error'),'error');
return;
}
const cleanCfg=cloneConfig(cfg);
delete cleanCfg.ok;
delete cleanCfg.status;
delete cleanCfg.error;
delete cleanCfg.raw;
currentConfig=cleanCfg;
populateForm(currentConfig);
updateRawPreview();
}

async function saveConfig(){
const validationError=validateConfigForm();
if(validationError){
setCfgMessage(validationError,'error');
return;
}
const nextCfg=collectConfigFromForm();
const result=await j('/api/admin/config',{method:'POST',body:JSON.stringify(nextCfg)});
if(result.ok){
setCfgMessage('Config saved.','success');
await loadConfig();
await loadStatus();
return;
}
setCfgMessage('Save failed: '+(result.error||JSON.stringify(result)),'error');
}

async function storeSecret(name,value){
if(!name||!value)return {ok:false,error:'name and value are required'};
return j('/api/admin/secrets',{method:'POST',body:JSON.stringify({name:name,value:value})});
}

async function setSecret(){
const name=byId('sname').value.trim();
const value=byId('svalue').value;
if(!name||!value){alert('Name and value required');return;}
const result=await storeSecret(name,value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok){
byId('svalue').value='';
}
}

async function storeProviderSecret(){
const name=byId('providerSecretName').value.trim();
const value=byId('providerSecretValue').value;
if(!value){alert('Provider key value required');return;}
const result=await storeSecret(name,value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok)byId('providerSecretValue').value='';
}

async function storeDiscordToken(){
const value=byId('discordTokenValue').value;
if(!value){alert('Discord token value required');return;}
const result=await storeSecret('discord/bot_token',value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok)byId('discordTokenValue').value='';
}

async function storeTelegramToken(){
const value=byId('telegramTokenValue').value;
if(!value){alert('Telegram token value required');return;}
const result=await storeSecret('telegram/bot_token',value);
byId('secrets').textContent=JSON.stringify(result,null,2);
if(result.ok)byId('telegramTokenValue').value='';
}

async function listSecrets(){
byId('secrets').textContent=JSON.stringify(await j('/api/admin/secrets'),null,2);
}

function stripToolCalls(text){
if(!text)return'';
// Remove tool_call XML blocks - these are internal implementation details
// that should not be shown to users
text=text.replace(/<tool_call>\s*\/?>\s*\{[\s\S]*?\}/g,'');
text=text.replace(/<tool_call>[\s\S]*?<\/tool_call>/g,'');
text=text.replace(/<tool_call\s+[^>]*\/>/g,'');
return text;
}

function formatContent(text){
if(!text)return'';
// Strip tool calls before displaying to users
text=stripToolCalls(text);
text=text.replace(/\u0026/g,'\u0026amp;').replace(/\u003c/g,'\u0026lt;').replace(/\u003e/g,'\u0026gt;');
text=text.replace(/\u0060\u0060\u0060(\w+)?\n([\s\S]*?)\u0060\u0060\u0060/g,'\u003cpre\u003e\u003ccode\u003e$2\u003c/code\u003e\u003c/pre\u003e');
text=text.replace(/\u0060([^\u0060]+)\u0060/g,'\u003ccode\u003e$1\u003c/code\u003e');
text=text.replace(/\n/g,'\u003cbr\u003e');
return text;
}

function normalizeForSearch(value){
if(value===undefined||value===null)return'';
return String(value).toLowerCase();
}

function isChatNearBottom(container){
if(!container)return true;
const distance=container.scrollHeight-(container.scrollTop+container.clientHeight);
return distance<=36;
}

function renderChat(){
const container=byId('chatHistory');
const wasPinned=chatPinnedToBottom||isChatNearBottom(container);
const previousTop=container.scrollTop;
container.innerHTML=chatMessages.map(function(m){
const roleClass=m.role==='user'?'chat-user':'chat-assistant';
const roleLabel=m.role==='user'?'You':'Bot';
const content=formatContent(m.content);
return'<div class="chat-message '+roleClass+'"><strong>'+roleLabel+':</strong><br>'+content+'</div>';
}).join('');
if(wasPinned){
container.scrollTop=container.scrollHeight;
}else{
const maxTop=Math.max(0,container.scrollHeight-container.clientHeight);
container.scrollTop=Math.min(previousTop,maxTop);
}
chatPinnedToBottom=isChatNearBottom(container);
chatScrollTop=container.scrollTop;
}

function bindChatScrollTracking(){
const container=byId('chatHistory');
if(!container)return;
container.addEventListener('scroll',function(){
chatPinnedToBottom=isChatNearBottom(container);
chatScrollTop=container.scrollTop;
});
}

function renderToolActivity(){
const container=byId('toolHistory');
if(!container)return;
const filterInput=byId('toolFilter');
const query=normalizeForSearch(filterInput?filterInput.value.trim():'');
const statusMode=((byId('toolStatusFilter')&&byId('toolStatusFilter').value)||'all').toLowerCase();
if(toolActivity.length===0){
container.className='tool-empty';
container.textContent='No tool activity yet.';
return;
}
const filtered=toolActivity.filter(function(item){
if(statusMode==='errors' && !(item&&item.error))return false;
if(statusMode==='output' && (item&&item.error))return false;
if(!query)return true;
const blob=normalizeForSearch((item&&item.tool)||'')+' '+normalizeForSearch((item&&item.callID)||'')+' '+normalizeForSearch((item&&item.summary)||'')+' '+normalizeForSearch((item&&item.output)||'')+' '+normalizeForSearch((item&&item.error)||'');
return blob.indexOf(query)!==-1;
});
if(filtered.length===0){
container.className='tool-empty';
container.textContent='No tool activity matches this filter.';
return;
}
container.className='';
container.innerHTML=filtered.slice(-10).map(function(item){
const tool=item.tool||'unknown.tool';
const callID=item.callID?(' ['+item.callID+']'):'';
const label=item.error?'error':'output';
const payload=item.error?item.error:item.output;
const summary=item.summary?formatContent(item.summary):'';
if(summary){
return '<div class="tool-item"><strong>'+tool+callID+'</strong><br><span class="tool-preview">'+summary+'</span></div>';
}
return '<div class="tool-item"><strong>'+tool+callID+'</strong><br>'+renderToolPayload(label,payload)+'</div>';
}).join('');
}

function renderToolPayload(label,payload){
if(!payload)return formatContent(label+': (empty)');
const value=String(payload);
if(value.length<=180){
return formatContent(label+': '+value);
}
const shortText=formatContent(label+': '+value.slice(0,180)+'...');
const fullText=formatContent(value);
return '<div class="tool-preview">'+shortText+'</div><details><summary>Show full '+label+'</summary><div class="tool-full">'+fullText+'</div></details>';
}

function compactToolText(value,maxLen){
if(!value)return '';
const v=String(value).trim();
if(v.length<=maxLen)return v;
return v.slice(0,maxLen)+'...';
}

function parseToolOutputJSON(output){
if(!output)return null;
try{return JSON.parse(String(output));}catch(e){return null;}
}

function deriveToolSummary(tool,output,error,fallbackSummary){
if(fallbackSummary&&String(fallbackSummary).trim())return String(fallbackSummary).trim();
if(error&&String(error).trim())return 'error: '+compactToolText(String(error),180);
const parsed=parseToolOutputJSON(output);
if(!parsed||typeof parsed!=='object')return '';
if(tool==='fs.write'){
const path=parsed.path?String(parsed.path).trim():'';
const lines=Number(parsed.lines);
if(path&&!Number.isNaN(lines))return 'wrote '+lines+' line(s) to '+path;
}
if(tool==='fs.edit'){
const path=parsed.path?String(parsed.path).trim():'';
const applied=Number(parsed.applied_edits);
if(path&&!Number.isNaN(applied))return 'applied '+applied+' edit(s) to '+path;
}
if(tool==='fs.list'){
const path=parsed.path?String(parsed.path).trim():'';
const entries=Array.isArray(parsed.entries)?parsed.entries.length:0;
if(path)return 'listed '+entries+' entries in '+path;
}
return '';
}

function appendToolActivityFromRun(run){
if(!run||!run.trace||!Array.isArray(run.trace.tool_execution_results))return;
run.trace.tool_execution_results.forEach(function(item){
if(!item||typeof item!=='object')return;
const tool=item.tool||'unknown.tool';
const output=compactToolText(item.output||'',5000);
const error=compactToolText(item.error||'',5000);
toolActivity.push({
tool:tool,
callID:item.tool_call_id||'',
summary:deriveToolSummary(tool,output,error,item.summary||''),
output:output,
error:error
});
});
renderToolActivity();
}

function toSessionLine(session){
const ts=session&&session.updated_at?new Date(session.updated_at):null;
const when=(ts&& !Number.isNaN(ts.getTime()))?ts.toLocaleString():'unknown time';
return {id:session.session_id||'',label:(session.session_id||'unknown')+' - '+when};
}

function renderSessionList(){
const container=byId('sessionList');
if(!container)return;
if(!Array.isArray(knownSessions)||knownSessions.length===0){
container.className='tool-empty';
container.textContent='No sessions yet.';
return;
}
const filterInput=byId('sessionFilter');
const query=normalizeForSearch(filterInput?filterInput.value.trim():'');
const filteredSessions=knownSessions.filter(function(session){
if(!query)return true;
const line=toSessionLine(session);
const blob=normalizeForSearch(line.id)+' '+normalizeForSearch(line.label)+' '+normalizeForSearch(session&&session.title?session.title:'');
return blob.indexOf(query)!==-1;
});
const sortMode=((byId('sessionSortMode')&&byId('sessionSortMode').value)||'recent').toLowerCase();
const sorted=filteredSessions.slice().sort(function(a,b){
const aTS=(a&&a.updated_at)?new Date(a.updated_at).getTime():0;
const bTS=(b&&b.updated_at)?new Date(b.updated_at).getTime():0;
if(sortMode==='oldest'){
return aTS-bTS;
}
if(sortMode==='active'){
const aActive=(a&&a.session_id===currentActiveSessionID)?1:0;
const bActive=(b&&b.session_id===currentActiveSessionID)?1:0;
if(aActive!==bActive)return bActive-aActive;
}
return bTS-aTS;
});
if(filteredSessions.length===0){
container.className='tool-empty';
container.textContent='No sessions match this filter.';
return;
}
container.className='';
container.innerHTML=sorted.slice(0,20).map(function(session){
const line=toSessionLine(session);
if(!line.id)return '';
const activeTag=(line.id===currentActiveSessionID)?'<span class="session-active-tag">active</span>':'';
const actionLabel=(line.id===currentActiveSessionID)?'Reopen':'Open';
return '<div class="session-item"><div class="session-meta">'+line.label+activeTag+'</div><div class="session-actions"><button onclick="openSession(\''+line.id+'\')">'+actionLabel+'</button></div></div>';
}).join('');
}

function maybeExtractSessionID(responseText){
const text=String(responseText||'').trim();
if(!text)return'';
let match=text.match(/^Started new chat:\s*(\S+)/i);
if(match&&match[1])return match[1];
match=text.match(/^Resumed chat:\s*(\S+)/i);
if(match&&match[1])return match[1];
return'';
}

async function refreshSessions(){
const data=await j('/api/admin/chat/sessions?agent_id=default&user_id=dashboard_user&room_id=dashboard&channel=dashboard');
if(!data||data.error||!Array.isArray(data.sessions)){
return;
}
knownSessions=data.sessions;
renderSessionList();
}

async function loadSessionMessages(sessionID){
const data=await j('/api/admin/chat/sessions/'+encodeURIComponent(sessionID)+'/messages?limit=200');
if(!data||data.error||!Array.isArray(data.messages)){
return;
}
chatMessages=[];
toolActivity=[];
data.messages.forEach(function(msg){
if(!msg||typeof msg!=='object')return;
if(msg.role==='tool'){
let parsed={};
try{parsed=JSON.parse(msg.content||'{}');}catch(e){parsed={};}
const toolName=msg.tool_name||parsed.tool||'unknown.tool';
const output=compactToolText(parsed.output||msg.content||'',5000);
const error=compactToolText(parsed.error||'',5000);
toolActivity.push({
tool:toolName,
callID:msg.tool_call_id||parsed.id||'',
summary:deriveToolSummary(toolName,output,error,parsed.summary||''),
output:output,
error:error
});
return;
}
if(msg.role==='assistant' || msg.role==='user'){
chatMessages.push({role:msg.role,content:msg.content||''});
}
});
renderToolActivity();
renderChat();
}

async function loadSessionToolActivity(sessionID){
if(!sessionID)return{count:0,lastSummary:''};
const data=await j('/api/admin/chat/sessions/'+encodeURIComponent(sessionID)+'/messages?limit=200');
if(!data||data.error||!Array.isArray(data.messages)){
return{count:0,lastSummary:''};
}
const nextToolActivity=[];
data.messages.forEach(function(msg){
if(!msg||typeof msg!=='object'||msg.role!=='tool')return;
let parsed={};
try{parsed=JSON.parse(msg.content||'{}');}catch(e){parsed={};}
const toolName=msg.tool_name||parsed.tool||'unknown.tool';
const output=compactToolText(parsed.output||msg.content||'',5000);
const error=compactToolText(parsed.error||'',5000);
nextToolActivity.push({
tool:toolName,
callID:msg.tool_call_id||parsed.id||'',
summary:deriveToolSummary(toolName,output,error,parsed.summary||''),
output:output,
error:error
});
});
toolActivity=nextToolActivity;
renderToolActivity();
const last=toolActivity.length>0?toolActivity[toolActivity.length-1]:null;
return{count:toolActivity.length,lastSummary:last&&(last.summary||last.error||last.tool)?(last.summary||last.error||last.tool):''};
}

async function openSession(sessionID){
if(!sessionID)return;
currentActiveSessionID=sessionID;
byId('resumeSessionID').value=sessionID;
await sendChatWithText('/resume '+sessionID);
await loadSessionMessages(sessionID);
}

async function pollRun(runId,maxSeconds){
const timeoutSeconds=(typeof maxSeconds==='number'&&maxSeconds>0)?maxSeconds:120;
for(let i=0;i<timeoutSeconds;i++){
await new Promise(function(resolve){setTimeout(resolve,1000);});
const run=await j('/v1/runs/'+runId);
if(run.status==='completed')return run;
if(run.status==='failed')return run;
}
return{status:'running',id:runId,error:'Still running after '+timeoutSeconds+'s'};
}

async function continuePollingRun(runId,thinkingIdx,startedAtMs){
const started=(typeof startedAtMs==='number'&&startedAtMs>0)?startedAtMs:Date.now();
for(;;){
let run;
let progress={count:0,lastSummary:''};
try{
if(currentActiveSessionID){
progress=await loadSessionToolActivity(currentActiveSessionID);
}
run=await pollRun(runId,5);
}catch(e){
chatMessages[thinkingIdx]={role:'assistant',content:'Run '+runId+' is still processing (temporary status check error: '+e.message+'). I will keep checking automatically.'};
renderChat();
await new Promise(function(resolve){setTimeout(resolve,3000);});
continue;
}
if(run.status==='failed'){
chatMessages[thinkingIdx]={role:'assistant',content:'Error: '+(run.error||'Run failed')};
renderChat();
loadStatus();
return;
}
if(run.status==='completed'){
const output=(run.output&&run.output.trim())?run.output:'Run completed without assistant output. Open trace or tool activity for details.';
chatMessages[thinkingIdx]={role:'assistant',content:output};
appendToolActivityFromRun(run);
renderChat();
loadStatus();
return;
}
const elapsedSeconds=Math.max(1,Math.floor((Date.now()-started)/1000));
let progressText='Run '+runId+' is still running ('+elapsedSeconds+'s elapsed).';
if(progress.count>0){
progressText+=' Completed '+progress.count+' tool call'+(progress.count===1?'':'s')+'.';
if(progress.lastSummary){
progressText+=' Latest: '+progress.lastSummary;
}
}
progressText+=' I am continuing to poll automatically and will post the final result here.';
chatMessages[thinkingIdx]={role:'assistant',content:progressText};
renderChat();
loadStatus();
await new Promise(function(resolve){setTimeout(resolve,3000);});
}
}

async function sendChat(){
await sendChatWithText('');
}

async function startNewChat(){
await sendChatWithText('/new');
}

async function listChats(){
await sendChatWithText('/chats');
}

async function resumeChat(){
const sessionID=byId('resumeSessionID').value.trim();
if(!sessionID){
alert('Session id required to resume chat');
return;
}
await sendChatWithText('/resume '+sessionID);
}

async function sendChatWithText(forcedMessage){
const input=byId('chatMessage');
const message=(forcedMessage&&forcedMessage.trim())?forcedMessage.trim():input.value.trim();
if(!message)return;
chatMessages.push({role:'user',content:message});
renderChat();
input.value='';
const thinkingIdx=chatMessages.length;
chatMessages.push({role:'assistant',content:'Thinking...'});
renderChat();
try{
const result=await j('/v1/chat/messages',{method:'POST',body:JSON.stringify({user_id:'dashboard_user',message:message,agent_id:'default'})});
if(result.error){
let errorText=String(result.error||'request failed');
if(result.status===403&&errorText.toLowerCase().indexOf('not allowlisted')!==-1){
errorText+=' (add "dashboard_user" to chat.allow_users)';
}
chatMessages[thinkingIdx]={role:'assistant',content:'Error: '+errorText};
}else if(result.id){
if(result.session_id&&String(result.session_id).trim()){
currentActiveSessionID=String(result.session_id).trim();
if(byId('resumeSessionID'))byId('resumeSessionID').value=currentActiveSessionID;
}
const initialStatus=(result.status&&String(result.status).trim())?String(result.status).trim():'queued';
chatMessages[thinkingIdx]={role:'assistant',content:'Working on it now. Run '+result.id+' is '+initialStatus+'. I will keep polling automatically and post the final output here.'};
renderChat();
continuePollingRun(result.id,thinkingIdx,Date.now());
}else if(result.response){
chatMessages[thinkingIdx]={role:'assistant',content:result.response};
const extractedSessionID=maybeExtractSessionID(result.response);
if(extractedSessionID){
currentActiveSessionID=extractedSessionID;
if(byId('resumeSessionID'))byId('resumeSessionID').value=extractedSessionID;
}
}else{
chatMessages[thinkingIdx]={role:'assistant',content:JSON.stringify(result)};
}
if(message==='/new' && !result.error){
toolActivity=[];
renderToolActivity();
}
if(message==='/new' || message==='/resume' || message.indexOf('/resume ')===0 || message==='/chats'){
await refreshSessions();
}
renderChat();
loadStatus();
}catch(e){
chatMessages[thinkingIdx]={role:'assistant',content:'Error: '+e.message};
renderChat();
}
}

function wireFormPreviewUpdates(){
['cfgProvider','cfgModelName','cfgTemperature','cfgGenericBaseURL','cfgChatEnabled','cfgDiscordEnabled','cfgTelegramEnabled','cfgSandboxActive','cfgShellExecEnabled','cfgChatUsers','cfgChatRooms','cfgDiscordUsers','cfgDiscordChannels','cfgDiscordGuilds','cfgTelegramUsers','cfgTelegramChats'].forEach(function(id){
const el=byId(id);
if(!el)return;
el.addEventListener('input',updateRawPreview);
el.addEventListener('change',updateRawPreview);
});
}

wireFormPreviewUpdates();
applyLayoutPreferences();
bindChatResizePersistence();
bindChatScrollTracking();
renderToolActivity();
refreshSessions();
loadStatus();
loadConfig();
setInterval(loadStatus,30000);
</script></body></html>`
