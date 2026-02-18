package chatstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkReadRecentMessages(b *testing.B) {
	agentsRoot := filepath.Join(b.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		b.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "benchmark",
		UserID:  "user1",
		RoomID:  "room1",
	})
	if err != nil {
		b.Fatalf("create session: %v", err)
	}

	const numMessages = 10000
	for i := 0; i < numMessages; i++ {
		msg := Message{Role: "user", Content: fmt.Sprintf("message content %d", i)}
		if err := store.AppendMessage(session.SessionID, msg); err != nil {
			b.Fatalf("append message: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgs, err := store.ReadRecentMessages(session.SessionID, 50)
		if err != nil {
			b.Fatalf("read recent messages: %v", err)
		}
		if len(msgs) != 50 {
			b.Fatalf("expected 50 messages, got %d", len(msgs))
		}
	}
}

func BenchmarkListSessions(b *testing.B) {
	agentsRoot := filepath.Join(b.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		b.Fatalf("new store: %v", err)
	}

	for i := 0; i < 1000; i++ {
		_, err := store.CreateSession(CreateSessionInput{
			AgentID: "default",
			Channel: "benchmark",
			UserID:  fmt.Sprintf("user-%d", i%10),
			RoomID:  "room1",
			Title:   fmt.Sprintf("session-%d", i),
		})
		if err != nil {
			b.Fatalf("create session %d: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessions, err := store.ListSessions("default", "", "", "")
		if err != nil {
			b.Fatalf("list sessions: %v", err)
		}
		if len(sessions) != 1000 {
			b.Fatalf("expected 1000 sessions, got %d", len(sessions))
		}
	}
}
