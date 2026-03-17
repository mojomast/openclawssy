package httpchannel

import (
	"testing"
	"time"
)

func TestRunEventBusSubscribePublishAndUnsubscribe(t *testing.T) {
	bus := NewRunEventBus(16)
	ch, unsubscribe := bus.Subscribe("run_stream_1", 0)

	bus.Publish("run_stream_1", RunEvent{Type: RunEventStatus, Data: map[string]any{"status": "running"}})

	select {
	case event := <-ch:
		if event.ID != 1 {
			t.Fatalf("expected event id=1, got %d", event.ID)
		}
		if event.Type != RunEventStatus {
			t.Fatalf("expected status event, got %q", event.Type)
		}
		if event.RunID != "run_stream_1" {
			t.Fatalf("expected run id run_stream_1, got %q", event.RunID)
		}
		if event.InstanceID != "" || event.AgentID != "" {
			t.Fatalf("expected empty identity metadata by default, got %+v", event)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for published event")
	}

	unsubscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed subscriber channel after unsubscribe")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for unsubscribe close")
	}
}

func TestRunEventBusCarriesInstanceAndAgentIdentity(t *testing.T) {
	bus := NewRunEventBus(16)
	ch, unsubscribe := bus.Subscribe("run_stream_identity", 0)
	defer unsubscribe()

	bus.Publish("run_stream_identity", RunEvent{Type: RunEventStatus, InstanceID: "instance-a", AgentID: "agent-7", Data: map[string]any{"status": "running"}})

	select {
	case event := <-ch:
		if event.InstanceID != "instance-a" {
			t.Fatalf("expected instance_id instance-a, got %q", event.InstanceID)
		}
		if event.AgentID != "agent-7" {
			t.Fatalf("expected agent_id agent-7, got %q", event.AgentID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for published identity event")
	}
}

func TestRunEventBusSubscribeCompositePublishesBareRunID(t *testing.T) {
	bus := NewRunEventBus(16)
	ch, unsubscribe := bus.SubscribeComposite("instance-a", "agent-7", "run_stream_identity", 0)
	defer unsubscribe()

	bus.PublishComposite("instance-a", "agent-7", "run_stream_identity", RunEvent{Type: RunEventCompleted, InstanceID: "instance-a", AgentID: "agent-7", Data: map[string]any{"status": "completed"}})
	bus.CloseComposite("instance-a", "agent-7", "run_stream_identity")

	select {
	case event, ok := <-ch:
		if !ok {
			t.Fatal("expected composite event before channel close")
		}
		if event.RunID != "run_stream_identity" {
			t.Fatalf("expected public run id run_stream_identity, got %q", event.RunID)
		}
		if event.InstanceID != "instance-a" || event.AgentID != "agent-7" {
			t.Fatalf("expected identity metadata, got %+v", event)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for composite event")
	}
}

func TestRunEventBusReplaysEventsAfterLastEventID(t *testing.T) {
	bus := NewRunEventBus(16)
	runID := "run_stream_replay"

	bus.Publish(runID, RunEvent{Type: RunEventStatus, Data: map[string]any{"status": "running"}})
	bus.Publish(runID, RunEvent{Type: RunEventToolEnd, Data: map[string]any{"tool": "fs.list"}})
	bus.Publish(runID, RunEvent{Type: RunEventCompleted, Data: map[string]any{"status": "completed"}})
	bus.Close(runID)

	ch, unsubscribe := bus.Subscribe(runID, 1)
	defer unsubscribe()

	var events []RunEvent
	for event := range ch {
		events = append(events, event)
	}
	if len(events) != 2 {
		t.Fatalf("expected two replayed events (> last_event_id), got %d", len(events))
	}
	if events[0].Type != RunEventToolEnd || events[0].ID != 2 {
		t.Fatalf("unexpected first replay event: %+v", events[0])
	}
	if events[1].Type != RunEventCompleted || events[1].ID != 3 {
		t.Fatalf("unexpected second replay event: %+v", events[1])
	}
}

func TestRunEventBusTerminalSubscribeClosesAndReplaysTerminalEvent(t *testing.T) {
	bus := NewRunEventBus(16)
	runID := "run_stream_terminal"

	bus.Publish(runID, RunEvent{Type: RunEventStatus, Data: map[string]any{"status": "running"}})
	bus.Publish(runID, RunEvent{Type: RunEventCompleted, Data: map[string]any{"status": "completed"}})
	bus.Close(runID)

	ch, unsubscribe := bus.Subscribe(runID, 99)
	defer unsubscribe()

	var events []RunEvent
	for event := range ch {
		events = append(events, event)
	}
	if len(events) != 1 {
		t.Fatalf("expected terminal replay event when already complete, got %d", len(events))
	}
	if events[0].Type != RunEventCompleted {
		t.Fatalf("expected completed replay event, got %q", events[0].Type)
	}
}
