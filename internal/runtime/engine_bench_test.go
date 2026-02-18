package runtime

import (
	"strconv"
	"testing"

	"openclawssy/internal/chatstore"
)

func BenchmarkLoadSessionMessages(b *testing.B) {
	root := b.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		b.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		b.Fatalf("init: %v", err)
	}

	// Use the engine's store to populate sessions so the index is updated.
	// This simulates a long-running process handling new sessions.
	store := e.chatStore
	if store == nil {
		b.Fatal("engine chat store is nil")
	}

	for i := 0; i < 100; i++ {
		session, err := store.CreateSession(chatstore.CreateSessionInput{
			AgentID: "default",
			Channel: "dashboard",
			UserID:  "user" + strconv.Itoa(i),
			RoomID:  "room" + strconv.Itoa(i),
		})
		if err != nil {
			b.Fatalf("create session: %v", err)
		}
		if err := store.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "hello"}); err != nil {
			b.Fatalf("append message: %v", err)
		}
	}

	// Create a target session for benchmarking
	targetSession, err := store.CreateSession(chatstore.CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "bench_user",
		RoomID:  "bench_room",
	})
	if err != nil {
		b.Fatalf("create target session: %v", err)
	}
	for i := 0; i < 50; i++ {
		if err := store.AppendMessage(targetSession.SessionID, chatstore.Message{Role: "user", Content: "bench message " + strconv.Itoa(i)}); err != nil {
			b.Fatalf("append bench message: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.loadSessionMessages(targetSession.SessionID, 200)
		if err != nil {
			b.Fatalf("load session messages: %v", err)
		}
	}
}
