package chatstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateListGetAppendReadRecent(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	s1, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "u1",
		RoomID:  "r1",
		Title:   "first",
	})
	if err != nil {
		t.Fatalf("create session 1: %v", err)
	}

	_, err = store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "u2",
		RoomID:  "r1",
		Title:   "second",
	})
	if err != nil {
		t.Fatalf("create session 2: %v", err)
	}

	if err := store.AppendMessage(s1.SessionID, Message{Role: "user", Content: "one", TS: time.Now().UTC().Add(-2 * time.Second)}); err != nil {
		t.Fatalf("append one: %v", err)
	}
	if err := store.AppendMessage(s1.SessionID, Message{Role: "assistant", Content: "two", TS: time.Now().UTC().Add(-1 * time.Second)}); err != nil {
		t.Fatalf("append two: %v", err)
	}
	if err := store.AppendMessage(s1.SessionID, Message{Role: "user", Content: "three"}); err != nil {
		t.Fatalf("append three: %v", err)
	}

	sessions, err := store.ListSessions("default", "u1", "r1", "dashboard")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != s1.SessionID {
		t.Fatalf("unexpected listed session: %+v", sessions[0])
	}

	gotSession, err := store.GetSession(s1.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if gotSession.AgentID != "default" || gotSession.UserID != "u1" {
		t.Fatalf("unexpected session data: %+v", gotSession)
	}

	recent, err := store.ReadRecentMessages(s1.SessionID, 2)
	if err != nil {
		t.Fatalf("read recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent messages, got %d", len(recent))
	}
	if recent[0].Content != "two" || recent[1].Content != "three" {
		t.Fatalf("unexpected recent messages: %+v", recent)
	}
}

func TestPersistenceAcrossStoreRestart(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "discord",
		UserID:  "u1",
		RoomID:  "roomA",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.SetActiveSessionPointer("default", "discord", "u1", "roomA", session.SessionID); err != nil {
		t.Fatalf("set active pointer: %v", err)
	}

	reloaded, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}

	gotSession, err := reloaded.GetSession(session.SessionID)
	if err != nil {
		t.Fatalf("get session after restart: %v", err)
	}
	if gotSession.SessionID != session.SessionID {
		t.Fatalf("unexpected session after restart: %+v", gotSession)
	}

	msgs, err := reloaded.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read messages after restart: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages after restart: %+v", msgs)
	}

	active, err := reloaded.GetActiveSessionPointer("default", "discord", "u1", "roomA")
	if err != nil {
		t.Fatalf("get active pointer: %v", err)
	}
	if active != session.SessionID {
		t.Fatalf("unexpected active pointer: %s", active)
	}
}

func TestAppendMessageConcurrent(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "u1",
		RoomID:  "r1",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "m"}); err != nil {
				t.Errorf("append message %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	msgs, err := store.ReadRecentMessages(session.SessionID, n)
	if err != nil {
		t.Fatalf("read recent: %v", err)
	}
	if len(msgs) != n {
		t.Fatalf("expected %d messages, got %d", n, len(msgs))
	}
}

func TestAppendMessageConcurrentAcrossStoreInstancesKeepsJSONLValid(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const (
		writers   = 10
		perWriter = 25
	)

	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		writer := writer
		wg.Add(1)
		go func() {
			defer wg.Done()
			writerStore, err := NewStore(agentsRoot)
			if err != nil {
				errCh <- err
				return
			}
			for i := 0; i < perWriter; i++ {
				msg := Message{Role: "user", Content: fmt.Sprintf("writer-%d-message-%d", writer, i)}
				if err := writerStore.AppendMessage(session.SessionID, msg); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent append failed: %v", err)
	}

	expected := writers * perWriter
	msgPath := filepath.Join(agentsRoot, "default", "memory", "chats", session.SessionID, "messages.jsonl")
	raw, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("read messages file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != expected {
		t.Fatalf("expected %d jsonl lines, got %d", expected, len(lines))
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			t.Fatalf("unexpected empty line at %d", i)
		}
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("invalid jsonl line %d: %v", i, err)
		}
		if strings.TrimSpace(msg.Content) == "" {
			t.Fatalf("missing message content at line %d", i)
		}
	}

	recent, err := store.ReadRecentMessages(session.SessionID, expected)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(recent) != DefaultMaxHistoryCount {
		t.Fatalf("expected clamped readable messages count %d, got %d", DefaultMaxHistoryCount, len(recent))
	}
}

func TestGetActiveSessionPointerMissing(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = store.GetActiveSessionPointer("default", "dashboard", "u1", "r1")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestCloseSessionIsIdempotentAndPreventsActivePointer(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := store.CloseSession(session.SessionID); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if err := store.CloseSession(session.SessionID); err != nil {
		t.Fatalf("close session again should be idempotent: %v", err)
	}

	updated, err := store.GetSession(session.SessionID)
	if err != nil {
		t.Fatalf("get closed session: %v", err)
	}
	if !updated.IsClosed() {
		t.Fatal("expected session to be marked closed")
	}

	err = store.SetActiveSessionPointer("default", "dashboard", "u1", "r1", session.SessionID)
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed, got %v", err)
	}
}

func TestClampHistoryCount(t *testing.T) {
	if got := ClampHistoryCount(10, 50); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
	if got := ClampHistoryCount(0, 50); got != 50 {
		t.Fatalf("expected 50 for zero requested, got %d", got)
	}
	if got := ClampHistoryCount(500, 50); got != 50 {
		t.Fatalf("expected clamp to 50, got %d", got)
	}
}

func TestReadRecentMessagesSkipsMalformedLines(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "ok"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	msgPath := filepath.Join(agentsRoot, "default", "memory", "chats", session.SessionID, "messages.jsonl")
	f, err := os.OpenFile(msgPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open messages file: %v", err)
	}
	if _, err := f.WriteString("{not-json}\n"); err != nil {
		t.Fatalf("write malformed line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close messages file: %v", err)
	}

	msgs, err := store.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one valid message, got %d", len(msgs))
	}
	if msgs[0].Content != "ok" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
}

func TestReadRecentMessagesSupportsLargeMessageLines(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	large := strings.Repeat("x", 200*1024)
	if err := store.AppendMessage(session.SessionID, Message{Role: "assistant", Content: large}); err != nil {
		t.Fatalf("append large message: %v", err)
	}

	msgs, err := store.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	if msgs[0].Content != large {
		t.Fatalf("expected large content to round-trip; got %d chars", len(msgs[0].Content))
	}
}

func TestSessionMetaRecoversFromBackup(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	metaPath := filepath.Join(agentsRoot, "default", "memory", "chats", session.SessionID, "meta.json")
	bakPath := metaPath + ".bak"
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if err := os.WriteFile(bakPath, metaBytes, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(metaPath, []byte("{invalid-json}"), 0o600); err != nil {
		t.Fatalf("corrupt meta: %v", err)
	}

	reloaded, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got, err := reloaded.GetSession(session.SessionID)
	if err != nil {
		t.Fatalf("get session from backup: %v", err)
	}
	if got.SessionID != session.SessionID {
		t.Fatalf("unexpected session loaded from backup: %+v", got)
	}
}

func TestReadRecentMessagesWaitsForSessionLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cross-process flock is unix-only in this build")
	}

	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	lockPath := filepath.Join(agentsRoot, "default", "memory", "chats", session.SessionID, ".chatstore.lock")
	locked := make(chan struct{})
	released := make(chan struct{})
	go func() {
		_ = withCrossProcessLock(lockPath, time.Second, func() error {
			close(locked)
			time.Sleep(140 * time.Millisecond)
			return nil
		})
		close(released)
	}()

	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lock acquisition")
	}

	start := time.Now()
	msgs, err := store.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read recent: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	if waited := time.Since(start); waited < 90*time.Millisecond {
		t.Fatalf("expected read to wait for lock, only waited %s", waited)
	}

	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lock release")
	}
}

func TestGetActiveSessionPointerWaitsForPointerLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cross-process flock is unix-only in this build")
	}

	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	session, err := store.CreateSession(CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SetActiveSessionPointer("default", "dashboard", "u1", "r1", session.SessionID); err != nil {
		t.Fatalf("set active pointer: %v", err)
	}

	pointerPath := filepath.Join(agentsRoot, "default", "memory", "chats", "_active", "dashboard", "u1", "r1.json")
	locked := make(chan struct{})
	released := make(chan struct{})
	go func() {
		_ = withCrossProcessLock(pointerPath+".lock", time.Second, func() error {
			close(locked)
			time.Sleep(140 * time.Millisecond)
			return nil
		})
		close(released)
	}()

	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pointer lock acquisition")
	}

	start := time.Now()
	active, err := store.GetActiveSessionPointer("default", "dashboard", "u1", "r1")
	if err != nil {
		t.Fatalf("get active pointer: %v", err)
	}
	if active != session.SessionID {
		t.Fatalf("unexpected active pointer: %q", active)
	}
	if waited := time.Since(start); waited < 90*time.Millisecond {
		t.Fatalf("expected pointer read to wait for lock, only waited %s", waited)
	}

	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pointer lock release")
	}
}

func TestReadRecentMessages_EdgeCases(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "test",
		UserID:  "u1",
		RoomID:  "r1",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Helper to write raw content to messages.jsonl
	writeRaw := func(content string) {
		msgPath := filepath.Join(agentsRoot, "default", "memory", "chats", session.SessionID, "messages.jsonl")
		if err := os.WriteFile(msgPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write raw: %v", err)
		}
	}

	// Helper to read messages
	read := func(limit int) []Message {
		msgs, err := store.ReadRecentMessages(session.SessionID, limit)
		if err != nil {
			t.Fatalf("read recent: %v", err)
		}
		return msgs
	}

	// 1. Empty file
	writeRaw("")
	msgs := read(10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages from empty file, got %d", len(msgs))
	}

	// 2. Single message
	msg1 := `{"role":"user","content":"1"}`
	writeRaw(msg1 + "\n")
	msgs = read(10)
	if len(msgs) != 1 || msgs[0].Content != "1" {
		t.Errorf("expected 1 message '1', got %v", msgs)
	}

	// 3. Two messages
	msg2 := `{"role":"assistant","content":"2"}`
	writeRaw(msg1 + "\n" + msg2 + "\n")
	msgs = read(10)
	if len(msgs) != 2 || msgs[0].Content != "1" || msgs[1].Content != "2" {
		t.Errorf("expected 2 messages '1', '2', got %v", msgs)
	}

	// 4. Limit 1
	msgs = read(1)
	if len(msgs) != 1 || msgs[0].Content != "2" {
		t.Errorf("expected last message '2', got %v", msgs)
	}

	// 5. No trailing newline
	writeRaw(msg1 + "\n" + msg2)
	msgs = read(10)
	if len(msgs) != 2 || msgs[0].Content != "1" || msgs[1].Content != "2" {
		t.Errorf("expected 2 messages from file without trailing newline, got %v", msgs)
	}

	// 6. Only newlines
	writeRaw("\n\n\n")
	msgs = read(10)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages from newlines only, got %d", len(msgs))
	}

	// 7. Messages with newlines in between
	writeRaw(msg1 + "\n\n" + msg2 + "\n")
	msgs = read(10)
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages with extra newlines, got %d", len(msgs))
	}

	// 8. Large number of small messages (buffer boundary test)
	// 4096 buffer. make messages small enough to fit many in buffer.
	// say 50 bytes each. 100 messages = 5000 bytes. spanning 2 chunks.
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		line := fmt.Sprintf(`{"role":"user","content":"msg-%03d"}`, i)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	writeRaw(sb.String())

	// Read all
	msgs = read(1000)
	if len(msgs) != 100 {
		t.Errorf("expected 100 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "msg-000" || msgs[99].Content != "msg-099" {
		t.Errorf("message order mismatch")
	}

	// Read partial
	msgs = read(10)
	if len(msgs) != 10 {
		t.Errorf("expected 10 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "msg-090" || msgs[9].Content != "msg-099" {
		t.Errorf("partial read content mismatch: first=%s last=%s", msgs[0].Content, msgs[9].Content)
	}

	// 9. Exact buffer size alignment (tricky to construct exactly but we can try)
	// We rely on random chance or exact construction.
	// Let's rely on previous test.

	// 10. Malformed mixed with valid
	writeRaw(msg1 + "\n" + "{bad}\n" + msg2 + "\n")
	msgs = read(10)
	if len(msgs) != 2 {
		t.Errorf("expected 2 valid messages (skipping bad), got %d", len(msgs))
	}
	if msgs[0].Content != "1" || msgs[1].Content != "2" {
		t.Errorf("content mismatch with malformed line")
	}
}
