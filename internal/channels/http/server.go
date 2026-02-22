package httpchannel

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"openclawssy/internal/config"
)

const (
	defaultAddr                    = "127.0.0.1:8080"
	maxRunRequestBodyBytes         = 256 * 1024
	maxChatMessageRequestBodyBytes = 64 * 1024
	defaultHTTPReadHeaderTimeout   = 5 * time.Second
	defaultHTTPReadTimeout         = 15 * time.Second
	defaultHTTPWriteTimeout        = 60 * time.Second
	defaultHTTPIdleTimeout         = 120 * time.Second
	listSortCreatedDesc            = "created_desc"
	listSortCreatedAsc             = "created_asc"
)

var sseHeartbeatInterval = 15 * time.Second

type Config struct {
	Addr        string
	BearerToken string
	Store       RunStore
	Executor    RunExecutor
	Chat        ChatConnector
	EventBus    *RunEventBus
	RegisterMux func(mux *http.ServeMux)
}

type Server struct {
	addr        string
	bearerToken string
	store       RunStore
	executor    RunExecutor
	chat        ChatConnector
	eventBus    *RunEventBus
	httpServer  *http.Server
}

type ChatMessage struct {
	UserID       string `json:"user_id"`
	RoomID       string `json:"room_id"`
	AgentID      string `json:"agent_id,omitempty"`
	Message      string `json:"message"`
	ThinkingMode string `json:"thinking_mode,omitempty"`
}

type ChatConnector interface {
	HandleMessage(ctx context.Context, msg ChatMessage) (ChatResponse, error)
}

type ChatResponse struct {
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
	Response  string `json:"response,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type ExecutionInput struct {
	AgentID      string
	Message      string
	Source       string
	SessionID    string
	ThinkingMode string
	OnProgress   func(eventType string, data map[string]any)
}

type RunExecutor interface {
	Execute(ctx context.Context, input ExecutionInput) (ExecutionResult, error)
}

type NopExecutor struct{}

func (NopExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	return ExecutionResult{Output: "queued"}, nil
}

type postRunRequest struct {
	AgentID      string `json:"agent_id"`
	Message      string `json:"message"`
	ThinkingMode string `json:"thinking_mode,omitempty"`
}

type postRunResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func NewServer(cfg Config) *Server {
	addr := cfg.Addr
	if addr == "" {
		addr = defaultAddr
	}
	store := cfg.Store
	if store == nil {
		store = NewInMemoryRunStore()
	}
	executor := cfg.Executor
	if executor == nil {
		executor = NopExecutor{}
	}
	eventBus := cfg.EventBus
	if eventBus == nil {
		eventBus = NewRunEventBus(0)
	}

	s := &Server{
		addr:        addr,
		bearerToken: cfg.BearerToken,
		store:       store,
		executor:    executor,
		chat:        cfg.Chat,
		eventBus:    eventBus,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", s.handleHealthz)
	mux.HandleFunc("/v1/runs/events/", s.handleRunEvents)
	mux.HandleFunc("/v1/runs", s.handleRuns)
	mux.HandleFunc("/v1/runs/", s.handleRunByID)
	mux.HandleFunc("/v1/chat/messages", s.handleChatMessage)
	if cfg.RegisterMux != nil {
		cfg.RegisterMux(mux)
	}

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           s.authMiddleware(mux),
		ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		ReadTimeout:       defaultHTTPReadTimeout,
		WriteTimeout:      defaultHTTPWriteTimeout,
		IdleTimeout:       defaultHTTPIdleTimeout,
	}

	return s
}

func (s *Server) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.chat == nil {
		writeErrorJSON(w, http.StatusNotFound, "chat.disabled", "chat connector is disabled", 0)
		return
	}

	var req ChatMessage
	if err := decodeJSONRequest(w, r, &req, maxChatMessageRequestBodyBytes); err != nil {
		writeJSONDecodeError(w, err, maxChatMessageRequestBodyBytes)
		return
	}

	req.UserID = strings.TrimSpace(req.UserID)
	req.Message = strings.TrimSpace(req.Message)
	if req.UserID == "" || req.Message == "" {
		writeErrorJSON(w, http.StatusBadRequest, "request.invalid_input", "user_id and message are required", 0)
		return
	}
	thinkingMode, err := normalizeThinkingModeFromRequest(req.ThinkingMode)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "request.invalid_thinking_mode", err.Error(), 0)
		return
	}
	req.ThinkingMode = thinkingMode

	req.RoomID = strings.TrimSpace(req.RoomID)
	if req.RoomID == "" {
		req.RoomID = "dashboard"
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	if req.AgentID == "" {
		req.AgentID = "default"
	}

	result, err := s.chat.HandleMessage(r.Context(), req)
	if err != nil {
		if isRateLimitedError(err) {
			writeErrorJSON(w, http.StatusTooManyRequests, "chat.rate_limited", err.Error(), retryAfterFromError(err))
			return
		}
		writeErrorJSON(w, http.StatusForbidden, "chat.rejected", err.Error(), 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	statusCode := http.StatusAccepted
	if result.ID == "" {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if strings.TrimSpace(s.bearerToken) == "" {
		return errors.New("bearer token is required")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.ListenAndServe()
	}()
	return s.wait(ctx, errCh)
}

func (s *Server) ListenAndServeTLS(ctx context.Context, certFile, keyFile string) error {
	if strings.TrimSpace(s.bearerToken) == "" {
		return errors.New("bearer token is required")
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.ListenAndServeTLS(certFile, keyFile)
	}()
	return s.wait(ctx, errCh)
}

func (s *Server) wait(ctx context.Context, errCh <-chan error) error {

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		if err := WaitForQueuedRuns(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown before in-flight runs drained: %w", err)
		}
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "ts": time.Now().UTC()})
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostRun(w, r)
	case http.MethodGet:
		s.handleListRuns(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePostRun(w http.ResponseWriter, r *http.Request) {

	var req postRunRequest
	if err := decodeJSONRequest(w, r, &req, maxRunRequestBodyBytes); err != nil {
		writeJSONDecodeError(w, err, maxRunRequestBodyBytes)
		return
	}

	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Message = strings.TrimSpace(req.Message)
	if req.AgentID == "" || req.Message == "" {
		writeErrorJSON(w, http.StatusBadRequest, "request.invalid_input", "agent_id and message are required", 0)
		return
	}
	thinkingMode, err := normalizeThinkingModeFromRequest(req.ThinkingMode)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "request.invalid_thinking_mode", err.Error(), 0)
		return
	}
	req.ThinkingMode = thinkingMode

	created, err := QueueRunWithOptions(
		r.Context(),
		s.store,
		s.executor,
		req.AgentID,
		req.Message,
		"http",
		"",
		req.ThinkingMode,
		QueueRunOptions{EventBus: s.eventBus},
	)
	if err != nil {
		if errors.Is(err, ErrQueueFull) {
			writeErrorJSON(w, http.StatusTooManyRequests, "queue.full", "run queue is full", 0)
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "queue.failed", "failed to queue run", 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(postRunResponse{ID: created.ID, Status: created.Status})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	agentFilter := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	limit, offset, err := parseListPagination(r, 50, 500)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "request.invalid_input", err.Error(), 0)
		return
	}
	sortOrder, err := parseListSortOrder(r.URL.Query().Get("sort"))
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "request.invalid_input", err.Error(), 0)
		return
	}

	runs, err := s.store.List(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "runs.list_failed", "failed to list runs", 0)
		return
	}

	filtered := make([]Run, 0, len(runs))
	for _, run := range runs {
		if statusFilter != "" && strings.ToLower(strings.TrimSpace(run.Status)) != statusFilter {
			continue
		}
		if agentFilter != "" && !strings.EqualFold(strings.TrimSpace(run.AgentID), agentFilter) {
			continue
		}
		filtered = append(filtered, run)
	}
	sortRunsByCreated(filtered, sortOrder)

	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := filtered[offset:end]

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"runs":     page,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"sort":     sortOrder,
		"agent_id": agentFilter,
	})
}

func parseListPagination(r *http.Request, defaultLimit, maxLimit int) (int, int, error) {
	limit := defaultLimit
	offset := 0
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > maxLimit {
			return 0, 0, fmt.Errorf("limit must be between 1 and %d", maxLimit)
		}
		limit = parsed
	}
	if rawOffset := strings.TrimSpace(r.URL.Query().Get("offset")); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			return 0, 0, errors.New("offset must be >= 0")
		}
		offset = parsed
	}
	return limit, offset, nil
}

func parseListSortOrder(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return listSortCreatedDesc, nil
	}
	switch value {
	case listSortCreatedDesc, listSortCreatedAsc:
		return value, nil
	default:
		return "", fmt.Errorf("sort must be one of %s|%s", listSortCreatedDesc, listSortCreatedAsc)
	}
}

func sortRunsByCreated(runs []Run, sortOrder string) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].ID < runs[j].ID
		}
		if sortOrder == listSortCreatedAsc {
			return runs[i].CreatedAt.Before(runs[j].CreatedAt)
		}
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
}

func normalizeThinkingModeFromRequest(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	normalized := config.NormalizeThinkingMode(value)
	if !config.IsValidThinkingMode(normalized) {
		return "", errors.New("thinking_mode must be one of never|on_error|always")
	}
	return normalized, nil
}

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, target any, maxBytes int) error {
	reader := r.Body
	if maxBytes > 0 {
		reader = http.MaxBytesReader(w, r.Body, int64(maxBytes))
	}

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("json body must contain exactly one object")
	}
	return nil
}

func writeJSONDecodeError(w http.ResponseWriter, err error, maxBytes int) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeErrorJSON(w, http.StatusRequestEntityTooLarge, "request.body_too_large", fmt.Sprintf("request body exceeds %d bytes", maxBytes), 0)
		return
	}
	writeErrorJSON(w, http.StatusBadRequest, "request.invalid_json", "invalid json body", 0)
}

func writeErrorJSON(w http.ResponseWriter, status int, code, message string, retryAfter time.Duration) {
	if code == "" {
		code = "request.error"
	}
	if message == "" {
		message = "request failed"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := errorResponse{Error: errorBody{Code: code, Message: message}}
	if retryAfter > 0 {
		seconds := int(retryAfter / time.Second)
		if retryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		resp.Error.RetryAfterSeconds = seconds
	}
	_ = json.NewEncoder(w).Encode(resp)
}

type retryAfterError interface {
	RetryAfter() time.Duration
}

func retryAfterFromError(err error) time.Duration {
	var retryErr retryAfterError
	if errors.As(err, &retryErr) {
		return retryErr.RetryAfter()
	}
	return 0
}

func isRateLimitedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "rate limited")
}

func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "run id is required", http.StatusBadRequest)
		return
	}

	run, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(run)
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.eventBus == nil {
		http.Error(w, "run event stream is disabled", http.StatusNotFound)
		return
	}

	runID := strings.TrimPrefix(r.URL.Path, "/v1/runs/events/")
	if !isValidRunID(runID) {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}

	lastEventID := int64(0)
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid Last-Event-ID", http.StatusBadRequest)
			return
		}
		lastEventID = parsed
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, unsubscribe := s.eventBus.Subscribe(runID, lastEventID)
	defer unsubscribe()

	heartbeatTicker := time.NewTicker(sseHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSEEventFrame(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeatTicker.C:
			if err := writeSSEHeartbeatFrame(w); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEventFrame(w http.ResponseWriter, event RunEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	eventType := strings.TrimSpace(string(event.Type))
	if eventType == "" {
		eventType = "message"
	}
	if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func writeSSEHeartbeatFrame(w http.ResponseWriter) error {
	payload, err := json.Marshal(map[string]any{"ts": time.Now().UTC()})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", RunEventHeartbeat); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func isValidRunID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnauthenticatedRoute(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			http.Error(w, "invalid authorization scheme", http.StatusUnauthorized)
			return
		}

		token := strings.TrimSpace(parts[1])
		if token == "" || !secureTokenEquals(token, s.bearerToken) {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func secureTokenEquals(got, expected string) bool {
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func isUnauthenticatedRoute(method, requestPath string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	if requestPath == "/v1/healthz" {
		return true
	}
	if requestPath == "/dashboard" || requestPath == "/dashboard-legacy" {
		return true
	}
	cleaned := path.Clean(requestPath)
	if strings.HasPrefix(cleaned, "/dashboard/static/") && strings.TrimPrefix(cleaned, "/dashboard/static/") != "" {
		return true
	}
	return false
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}
