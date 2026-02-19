package httpchannel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testChatConnector struct {
	err      error
	response ChatResponse
}

type rateLimitedTestError struct {
	retryAfter time.Duration
}

func (e rateLimitedTestError) Error() string {
	return "chat sender is rate limited"
}

func (e rateLimitedTestError) RetryAfter() time.Duration {
	return e.retryAfter
}

func (c testChatConnector) HandleMessage(ctx context.Context, msg ChatMessage) (ChatResponse, error) {
	_ = ctx
	_ = msg
	if c.err != nil {
		return ChatResponse{}, c.err
	}
	if c.response.ID != "" || c.response.Response != "" || c.response.Status != "" {
		return c.response, nil
	}
	return ChatResponse{ID: "run-chat", Status: "queued"}, nil
}

func TestServer_DefaultAddrIsLoopback(t *testing.T) {
	s := NewServer(Config{BearerToken: "token"})
	if s.Addr() != "127.0.0.1:8080" {
		t.Fatalf("expected loopback default addr, got %q", s.Addr())
	}
}

func TestServer_AuthRequired(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore()})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run_1", nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestServer_InvalidTokenRejected(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore()})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run_1", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestServer_PostAndGetRun(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}})

	body := bytes.NewBufferString(`{"agent_id":"agent-1","message":"hello"}`)
	postReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	postReq.Header.Set("Authorization", "Bearer secret")
	postRR := httptest.NewRecorder()

	s.Handler().ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, postRR.Code)
	}

	var postRes postRunResponse
	if err := json.Unmarshal(postRR.Body.Bytes(), &postRes); err != nil {
		t.Fatalf("decode post response: %v", err)
	}
	if postRes.ID == "" {
		t.Fatal("expected run id in response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/runs/"+postRes.ID, nil)
	getReq.Header.Set("Authorization", "Bearer secret")
	getRR := httptest.NewRecorder()

	s.Handler().ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, getRR.Code)
	}

	var run Run
	if err := json.Unmarshal(getRR.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if run.ID != postRes.ID {
		t.Fatalf("expected run id %q, got %q", postRes.ID, run.ID)
	}
}

func TestServer_ListRunsSupportsPaginationAndStatusFilter(t *testing.T) {
	store := NewInMemoryRunStore()
	now := time.Now().UTC()
	for _, run := range []Run{
		{ID: "run-1", AgentID: "agent-1", Message: "m1", Status: "queued", CreatedAt: now, UpdatedAt: now},
		{ID: "run-2", AgentID: "agent-1", Message: "m2", Status: "completed", CreatedAt: now, UpdatedAt: now},
		{ID: "run-3", AgentID: "agent-1", Message: "m3", Status: "completed", CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := store.Create(context.Background(), run); err != nil {
			t.Fatalf("create run %s: %v", run.ID, err)
		}
	}

	s := NewServer(Config{BearerToken: "secret", Store: store, Executor: NopExecutor{}})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs?status=completed&limit=1&offset=1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	var payload struct {
		Runs   []Run `json:"runs"`
		Total  int   `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 2 || payload.Limit != 1 || payload.Offset != 1 {
		t.Fatalf("unexpected pagination payload: %+v", payload)
	}
	if len(payload.Runs) != 1 || payload.Runs[0].ID != "run-3" {
		t.Fatalf("unexpected page content: %+v", payload.Runs)
	}
}

func TestServer_ListRunsRejectsInvalidPagination(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs?limit=0", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var resp errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error.Code != "request.invalid_input" {
		t.Fatalf("unexpected error code: %#v", resp)
	}
}

func TestServer_ListenAndServeRequiresToken(t *testing.T) {
	s := NewServer(Config{Store: NewInMemoryRunStore()})
	err := s.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("expected error when token is empty")
	}
}

func TestServer_ChatMessageEndpoint(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{}})
	body := bytes.NewBufferString(`{"user_id":"u1","room_id":"r1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rr.Code)
	}

	var resp ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.ID != "run-chat" || resp.Status != "queued" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestServer_ChatMessageIncludesSessionIDWhenProvided(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{response: ChatResponse{ID: "run-chat", Status: "queued", SessionID: "session-1"}}})
	body := bytes.NewBufferString(`{"user_id":"u1","room_id":"r1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rr.Code)
	}

	var resp ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.SessionID != "session-1" {
		t.Fatalf("expected session id in response, got %+v", resp)
	}
}

func TestServer_ChatMessageDenied(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{err: errors.New("denied")}})
	body := bytes.NewBufferString(`{"user_id":"u1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestServer_ChatMessageRateLimited(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{err: rateLimitedTestError{retryAfter: 2300 * time.Millisecond}}})
	body := bytes.NewBufferString(`{"user_id":"u1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected %d, got %d", http.StatusTooManyRequests, rr.Code)
	}

	var resp errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error.Code != "chat.rate_limited" {
		t.Fatalf("unexpected error code: %#v", resp)
	}
	if resp.Error.RetryAfterSeconds != 3 {
		t.Fatalf("expected retry_after_seconds=3, got %d", resp.Error.RetryAfterSeconds)
	}
}

func TestServer_ChatMessageImmediateResponse(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{response: ChatResponse{Response: "Started new chat: s1"}}})
	body := bytes.NewBufferString(`{"user_id":"u1","message":"/new"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	var resp ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.Response == "" || resp.ID != "" {
		t.Fatalf("unexpected immediate response: %+v", resp)
	}
}

func TestServer_ReturnsTooManyRequestsWhenRunQueueIsFull(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForQueuedRuns(ctx); err != nil {
		t.Fatalf("wait for prior queued runs: %v", err)
	}

	defaultQueuedRunTracker.mu.Lock()
	originalLimit := defaultQueuedRunTracker.maxInFlight
	defaultQueuedRunTracker.maxInFlight = 1
	defaultQueuedRunTracker.mu.Unlock()
	defer func() {
		defaultQueuedRunTracker.mu.Lock()
		defaultQueuedRunTracker.maxInFlight = originalLimit
		defaultQueuedRunTracker.mu.Unlock()
	}()

	release := make(chan struct{})
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: blockingExecutor{release: release}})

	first := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewBufferString(`{"agent_id":"agent-1","message":"first"}`))
	first.Header.Set("Authorization", "Bearer secret")
	firstResp := httptest.NewRecorder()
	s.Handler().ServeHTTP(firstResp, first)
	if firstResp.Code != http.StatusAccepted {
		t.Fatalf("expected first request accepted, got %d", firstResp.Code)
	}

	second := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewBufferString(`{"agent_id":"agent-1","message":"second"}`))
	second.Header.Set("Authorization", "Bearer secret")
	secondResp := httptest.NewRecorder()
	s.Handler().ServeHTTP(secondResp, second)
	if secondResp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected queue-full status %d, got %d", http.StatusTooManyRequests, secondResp.Code)
	}

	close(release)
	if err := WaitForQueuedRuns(ctx); err != nil {
		t.Fatalf("wait for queued runs: %v", err)
	}
}

func TestServer_PostRunAcceptsThinkingModeOverride(t *testing.T) {
	store := NewInMemoryRunStore()
	s := NewServer(Config{BearerToken: "secret", Store: store, Executor: NopExecutor{}})

	body := bytes.NewBufferString(`{"agent_id":"agent-1","message":"hello","thinking_mode":"always"}`)
	postReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	postReq.Header.Set("Authorization", "Bearer secret")
	postResp := httptest.NewRecorder()
	s.Handler().ServeHTTP(postResp, postReq)
	if postResp.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, postResp.Code)
	}

	var created postRunResponse
	if err := json.Unmarshal(postResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		run, err := store.Get(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status == "completed" {
			if run.ThinkingMode != "always" {
				t.Fatalf("expected persisted thinking_mode=always, got %q", run.ThinkingMode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete in time: status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServer_PostRunRejectsInvalidThinkingMode(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}})

	body := bytes.NewBufferString(`{"agent_id":"agent-1","message":"hello","thinking_mode":"sometimes"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}
	var resp errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error.Code != "request.invalid_thinking_mode" {
		t.Fatalf("unexpected error code: %#v", resp)
	}
}

func TestServer_ChatRejectsInvalidThinkingMode(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{}})
	body := bytes.NewBufferString(`{"user_id":"u1","room_id":"r1","message":"hello","thinking_mode":"invalid"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}
	var resp errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error.Code != "request.invalid_thinking_mode" {
		t.Fatalf("unexpected error code: %#v", resp)
	}
}

func TestServer_RunEventsSSERequiresAuth(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), EventBus: NewRunEventBus(16)})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/events/run_1", nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestServer_RunEventsSSEFrameFormat(t *testing.T) {
	eventBus := NewRunEventBus(16)
	runID := "run_stream_frame"
	eventBus.Publish(runID, RunEvent{Type: RunEventStatus, Data: map[string]any{"status": "running"}})
	eventBus.Close(runID)

	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), EventBus: eventBus})
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/runs/events/"+runID, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "id: 1") {
		t.Fatalf("expected id frame, got %q", text)
	}
	if !strings.Contains(text, "event: status") {
		t.Fatalf("expected event frame, got %q", text)
	}
	if !strings.Contains(text, `"type":"status"`) {
		t.Fatalf("expected serialized run event payload, got %q", text)
	}
}

func TestServer_RunEventsSSEHeartbeatEmission(t *testing.T) {
	original := sseHeartbeatInterval
	sseHeartbeatInterval = 20 * time.Millisecond
	defer func() { sseHeartbeatInterval = original }()

	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), EventBus: NewRunEventBus(16)})
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/runs/events/run_heartbeat", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	foundHeartbeat := false
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		line, readErr := reader.ReadString('\n')
		if strings.Contains(line, "event: heartbeat") {
			foundHeartbeat = true
			break
		}
		if readErr != nil {
			break
		}
	}
	cancel()
	if !foundHeartbeat {
		t.Fatal("expected heartbeat event in SSE stream")
	}
}

func TestServer_RunEventsLateSubscriberGetsTerminalReplay(t *testing.T) {
	eventBus := NewRunEventBus(16)
	runID := "run_stream_replay_terminal"
	eventBus.Publish(runID, RunEvent{Type: RunEventStatus, Data: map[string]any{"status": "running"}})
	eventBus.Publish(runID, RunEvent{Type: RunEventCompleted, Data: map[string]any{"status": "completed", "output": "done"}})
	eventBus.Close(runID)

	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), EventBus: eventBus})
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/runs/events/"+runID, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Last-Event-ID", "1")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "event: status") {
		t.Fatalf("did not expect stale status replay when Last-Event-ID=1: %q", text)
	}
	if !strings.Contains(text, "event: completed") {
		t.Fatalf("expected completed terminal replay, got %q", text)
	}
}

func TestServer_AuthAllowsOnlyDashboardGetHeadPathsWithoutToken(t *testing.T) {
	s := NewServer(Config{
		BearerToken: "secret",
		Store:       NewInMemoryRunStore(),
		RegisterMux: func(mux *http.ServeMux) {
			mux.HandleFunc("/dashboard", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			mux.HandleFunc("/dashboard-legacy", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			mux.HandleFunc("/dashboard/static/app.js", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		},
	})

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "dashboard get allowed", method: http.MethodGet, path: "/dashboard", want: http.StatusOK},
		{name: "dashboard head allowed", method: http.MethodHead, path: "/dashboard", want: http.StatusOK},
		{name: "dashboard legacy get allowed", method: http.MethodGet, path: "/dashboard-legacy", want: http.StatusOK},
		{name: "dashboard static get allowed", method: http.MethodGet, path: "/dashboard/static/app.js", want: http.StatusOK},
		{name: "dashboard static head allowed", method: http.MethodHead, path: "/dashboard/static/app.js", want: http.StatusOK},
		{name: "dashboard post blocked", method: http.MethodPost, path: "/dashboard", want: http.StatusUnauthorized},
		{name: "dashboard legacy post blocked", method: http.MethodPost, path: "/dashboard-legacy", want: http.StatusUnauthorized},
		{name: "dashboard static post blocked", method: http.MethodPost, path: "/dashboard/static/app.js", want: http.StatusUnauthorized},
		{name: "other path blocked", method: http.MethodGet, path: "/api/admin/status", want: http.StatusUnauthorized},
		{name: "admin memory path blocked", method: http.MethodGet, path: "/api/admin/memory/default", want: http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, rr.Code)
			}
		})
	}
}
