package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/chatstore"
	"openclawssy/internal/messagecontent"
)

func TestConnectorQueuesAllowedMessage(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	allow := NewAllowlist([]string{"u1"}, []string{"r1"})
	limiter := NewRateLimiter(5, time.Minute)
	connector := &Connector{
		Allowlist:      allow,
		RateLimiter:    limiter,
		DefaultAgentID: "default",
		Store:          store,
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = ctx
			if instanceID != "instance-a" {
				t.Fatalf("unexpected instance id: %q", instanceID)
			}
			if sessionID == "" {
				t.Fatal("expected session id")
			}
			if agentID != "default" || source != "chat" {
				t.Fatalf("unexpected queue args: agent=%s source=%s", agentID, source)
			}
			if !strings.Contains(message, "hello") {
				t.Fatalf("unexpected message: %s", message)
			}
			if thinkingMode != "" {
				t.Fatalf("expected empty thinking mode, got %q", thinkingMode)
			}
			if len(contentParts) != 0 {
				t.Fatalf("expected no content parts, got %#v", contentParts)
			}
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}

	run, err := connector.HandleMessage(context.Background(), Message{InstanceID: "instance-a", UserID: "u1", RoomID: "r1", Text: "hello"})
	if err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if run.ID != "run-1" {
		t.Fatalf("unexpected run id: %s", run.ID)
	}
	if !strings.Contains(run.Response, "run-1") || !strings.Contains(strings.ToLower(run.Response), "working on it") {
		t.Fatalf("expected working status response, got %q", run.Response)
	}
	if strings.TrimSpace(run.SessionID) == "" {
		t.Fatal("expected session id in connector result")
	}
	if run.InstanceID != "instance-a" || run.AgentID != "default" {
		t.Fatalf("expected composite identity in result, got %+v", run)
	}
}

func TestConnectorRejectsUnallowlisted(t *testing.T) {
	connector := &Connector{
		Allowlist: NewAllowlist([]string{"u1"}, nil),
		Store: func() *chatstore.Store {
			store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
			if err != nil {
				t.Fatalf("new chat store: %v", err)
			}
			return store
		}(),
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = contentParts
			return QueuedRun{}, nil
		},
	}

	_, err := connector.HandleMessage(context.Background(), Message{UserID: "u2", Text: "hello"})
	if err == nil {
		t.Fatal("expected allowlist error")
	}
}

func TestConnectorNewResumeAndChatsCommands(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	queued := 0
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = contentParts
			if sessionID == "" {
				t.Fatal("expected session id")
			}
			queued++
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}

	res, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/new"})
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	if !strings.HasPrefix(res.Response, "Started new chat: ") {
		t.Fatalf("unexpected /new response: %q", res.Response)
	}
	if strings.TrimSpace(res.SessionID) == "" {
		t.Fatal("expected /new to return session id")
	}
	if queued != 0 {
		t.Fatalf("expected no queued runs, got %d", queued)
	}

	sessionID := strings.TrimPrefix(res.Response, "Started new chat: ")
	res, err = connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/sessions"})
	if err != nil {
		t.Fatalf("sessions command: %v", err)
	}
	if !strings.Contains(res.Response, sessionID) {
		t.Fatalf("expected /sessions to include session id, got: %q", res.Response)
	}

	res, err = connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/resume " + sessionID})
	if err != nil {
		t.Fatalf("resume command: %v", err)
	}
	if res.Response != "Resumed chat: "+sessionID {
		t.Fatalf("unexpected /resume response: %q", res.Response)
	}
	if res.SessionID != sessionID {
		t.Fatalf("expected /resume session id %q, got %q", sessionID, res.SessionID)
	}
	if queued != 0 {
		t.Fatalf("expected no queued runs after commands, got %d", queued)
	}
}

func TestConnectorQueuesRawMessageAndStoresHistory(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	var queuedMessage string
	var queuedSessionID string
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			queuedMessage = message
			queuedSessionID = sessionID
			if thinkingMode != "always" {
				t.Fatalf("expected thinking mode override to propagate, got %q", thinkingMode)
			}
			if len(contentParts) != 0 {
				t.Fatalf("expected no content parts, got %#v", contentParts)
			}
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}

	if _, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "first", ThinkingMode: "always"}); err != nil {
		t.Fatalf("first message: %v", err)
	}
	second, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "second", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("second message: %v", err)
	}

	if queuedMessage != "second" {
		t.Fatalf("expected raw message, got: %q", queuedMessage)
	}
	if queuedSessionID == "" {
		t.Fatal("expected queued session id")
	}
	if strings.TrimSpace(second.Response) == "" {
		t.Fatal("expected non-empty queued response message")
	}

	sessions, err := store.ListSessions("default", "u1", "dashboard", "dashboard")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}

	msgs, err := store.ReadRecentMessages(sessions[0].SessionID, 10)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in store, got %d", len(msgs))
	}
	if msgs[0].Content != "first" || msgs[1].Content != "second" {
		t.Fatalf("unexpected stored messages: %+v", msgs)
	}
	if sessions[0].SessionID != queuedSessionID {
		t.Fatalf("expected queued session %q, got %q", sessions[0].SessionID, queuedSessionID)
	}
}

func TestConnectorGlobalRateLimiterReturnsCooldown(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		GlobalLimiter:  NewRateLimiterWithClock(1, time.Minute, clock),
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = contentParts
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}

	if _, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "first"}); err != nil {
		t.Fatalf("first message: %v", err)
	}
	_, err = connector.HandleMessage(context.Background(), Message{UserID: "u2", RoomID: "dashboard", Source: "dashboard", Text: "second"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rateErr.Scope != "global" {
		t.Fatalf("expected global scope, got %q", rateErr.Scope)
	}
	if rateErr.RetryAfterSeconds < 1 {
		t.Fatalf("expected cooldown in error, got %+v", rateErr)
	}
}

func TestConnectorClosedSessionGetsReplacedAndCannotResume(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	queuedSessionIDs := make([]string, 0, 2)
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = contentParts
			queuedSessionIDs = append(queuedSessionIDs, sessionID)
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}

	first, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "hello"})
	if err != nil {
		t.Fatalf("first message: %v", err)
	}
	if first.SessionID == "" {
		t.Fatal("expected first session id")
	}
	if err := store.CloseSession(first.SessionID); err != nil {
		t.Fatalf("close first session: %v", err)
	}

	second, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "next"})
	if err != nil {
		t.Fatalf("second message: %v", err)
	}
	if second.SessionID == "" || second.SessionID == first.SessionID {
		t.Fatalf("expected second message to use a new session, first=%q second=%q", first.SessionID, second.SessionID)
	}
	if len(queuedSessionIDs) != 2 || queuedSessionIDs[0] != first.SessionID || queuedSessionIDs[1] != second.SessionID {
		t.Fatalf("unexpected queued session ids: %#v", queuedSessionIDs)
	}

	res, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/resume " + first.SessionID})
	if err != nil {
		t.Fatalf("resume closed session: %v", err)
	}
	if !strings.Contains(res.Response, "Session is closed") {
		t.Fatalf("expected closed session message, got %q", res.Response)
	}
}

func TestConnectorAgentSwitchCommand(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	_, err = store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("seed default session: %v", err)
	}
	_, err = store.CreateSession(chatstore.CreateSessionInput{AgentID: "alpha", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("seed alpha session: %v", err)
	}

	queuedAgent := ""
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = message
			_ = source
			_ = sessionID
			_ = thinkingMode
			_ = contentParts
			queuedAgent = agentID
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}

	res, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/agent alpha"})
	if err != nil {
		t.Fatalf("agent switch command: %v", err)
	}
	if !strings.Contains(res.Response, "alpha") {
		t.Fatalf("expected switch response to mention alpha, got %q", res.Response)
	}

	if _, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "hello"}); err != nil {
		t.Fatalf("queue message after switch: %v", err)
	}
	if queuedAgent != "alpha" {
		t.Fatalf("expected queued agent alpha, got %q", queuedAgent)
	}
}

func TestConnectorQueuesContentPartsAndStoresHistory(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	var queuedParts []messagecontent.Part
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, instanceID, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error) {
			_ = ctx
			_ = agentID
			_ = source
			_ = sessionID
			_ = thinkingMode
			if !strings.Contains(message, "sticker") {
				t.Fatalf("expected text prompt preserved, got %q", message)
			}
			queuedParts = append([]messagecontent.Part(nil), contentParts...)
			return QueuedRun{InstanceID: instanceID, AgentID: agentID, ID: "run-1", Status: "queued"}, nil
		},
	}
	parts := []messagecontent.Part{{Type: messagecontent.TypeText, Text: "use this sticker"}, {Type: messagecontent.TypeImage, MIMEType: "image/webp", Data: "AAAA"}}
	if _, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "use this sticker", ContentParts: parts}); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if len(queuedParts) != 2 || queuedParts[1].Type != messagecontent.TypeImage {
		t.Fatalf("expected queued content parts, got %#v", queuedParts)
	}
	sessions, err := store.ListSessions("default", "u1", "dashboard", "dashboard")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	msgs, err := store.ReadRecentMessages(sessions[0].SessionID, 10)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) != 1 || len(msgs[0].ContentParts) != 2 {
		t.Fatalf("expected stored content parts, got %+v", msgs)
	}
}
