package runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"openclawssy/internal/agent"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/instances"
	"openclawssy/internal/policy"
	"openclawssy/internal/tools"
)

func TestProactiveSignalFromToolOutput(t *testing.T) {
	checkpoint := `{"result":{"new_items":[{"kind":"preference","content":"remind me every friday","importance":4}]}}`
	sig := proactiveSignalFromToolOutput("memory.checkpoint", checkpoint)
	if !sig.Trigger {
		t.Fatalf("expected checkpoint signal trigger, got %+v", sig)
	}

	maintenance := `{"archived_stale_count":2}`
	sig = proactiveSignalFromToolOutput("memory.maintenance", maintenance)
	if !sig.Trigger {
		t.Fatalf("expected maintenance signal trigger, got %+v", sig)
	}

	none := proactiveSignalFromToolOutput("fs.read", `{"ok":true}`)
	if none.Trigger {
		t.Fatalf("expected non-memory tool to not trigger, got %+v", none)
	}
}

func TestMaybeTriggerProactiveMemoryHookDispatchesAgentMessage(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init engine: %v", err)
	}
	if _, err := instances.BootstrapDefaultInstance(root); err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.ProactiveEnabled = true
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	enforcer := policy.NewEnforcer(e.workspaceDir, map[string][]string{"default": {"agent.message.send", "agent.message.inbox"}})
	reg := tools.NewRegistry(enforcer, nil)
	if err := tools.RegisterCoreWithOptions(reg, tools.CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, AgentsPath: e.agentsDir, ChatstorePath: e.agentsDir, WorkspaceRoot: e.workspaceDir}); err != nil {
		t.Fatalf("register core tools: %v", err)
	}

	session, err := e.chatStore.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u-1", RoomID: "room-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	e.maybeTriggerProactiveMemoryHook(context.Background(), cfg, reg, "default", "default", session.SessionID, "run_123", agent.ToolCallRecord{
		Request: agent.ToolCallRequest{Name: "memory.maintenance"},
		Result:  agent.ToolCallResult{Output: `{"archived_stale_count":1}`},
	})

	ctx := tools.WithRequestContext(context.Background(), tools.RequestContext{InstanceID: "default"})
	inbox, err := reg.Execute(ctx, "default", "agent.message.inbox", e.workspaceDir, map[string]any{"agent_id": "default", "limit": 5})
	if err != nil {
		t.Fatalf("agent.message.inbox: %v", err)
	}
	if inbox["instance_id"] != "default" {
		t.Fatalf("expected inbox instance_id=default, got %#v", inbox)
	}
	var entry map[string]any
	if typed, ok := inbox["messages"].([]map[string]any); ok {
		if len(typed) == 0 {
			t.Fatalf("expected inbox messages, got %#v", inbox["messages"])
		}
		entry = typed[0]
	} else {
		rawMessages, ok := inbox["messages"].([]any)
		if !ok || len(rawMessages) == 0 {
			t.Fatalf("expected inbox messages, got %#v", inbox["messages"])
		}
		var castOK bool
		entry, castOK = rawMessages[0].(map[string]any)
		if !castOK {
			t.Fatalf("expected map entry, got %#v", rawMessages[0])
		}
	}
	content, _ := entry["content"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("decode proactive payload: %v", err)
	}
	if entry["instance_id"] != "default" {
		t.Fatalf("expected proactive envelope instance_id=default, got %#v", entry)
	}
	if payload["instance_id"] != "default" {
		t.Fatalf("expected proactive payload instance_id=default, got %#v", payload)
	}
	if payload["session_id"] != session.SessionID {
		t.Fatalf("expected proactive payload session id %q, got %#v", session.SessionID, payload["session_id"])
	}
	if payload["channel"] != "dashboard" || payload["user_id"] != "u-1" {
		t.Fatalf("expected channel/user context in payload, got %#v", payload)
	}
}

func TestProactiveHookSkipsWhenSessionIDEmpty(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init engine: %v", err)
	}

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.ProactiveEnabled = true
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	enforcer := policy.NewEnforcer(e.workspaceDir, map[string][]string{"default": {"agent.message.send", "agent.message.inbox"}})
	reg := tools.NewRegistry(enforcer, nil)
	if err := tools.RegisterCoreWithOptions(reg, tools.CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, AgentsPath: e.agentsDir, ChatstorePath: e.agentsDir, WorkspaceRoot: e.workspaceDir}); err != nil {
		t.Fatalf("register core tools: %v", err)
	}

	// Call with empty session ID — should not panic or send message.
	e.maybeTriggerProactiveMemoryHook(context.Background(), cfg, reg, "default", "default", "", "run_123", agent.ToolCallRecord{
		Request: agent.ToolCallRequest{Name: "memory.maintenance"},
		Result:  agent.ToolCallResult{Output: `{"archived_stale_count":1}`},
	})

	inbox, err := reg.Execute(tools.WithRequestContext(context.Background(), tools.RequestContext{InstanceID: "default"}), "default", "agent.message.inbox", e.workspaceDir, map[string]any{"agent_id": "default", "limit": 5})
	if err != nil {
		t.Fatalf("agent.message.inbox: %v", err)
	}
	// Should have NO messages since session context was missing.
	messages, _ := inbox["messages"].([]any)
	typedMessages, _ := inbox["messages"].([]map[string]any)
	if len(messages) > 0 || len(typedMessages) > 0 {
		t.Fatalf("expected no proactive messages when session context missing, got %#v", inbox["messages"])
	}
}

func TestProactiveHookSkipsWhenMemoryDisabled(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	cfg := config.Default()
	cfg.Memory.Enabled = false
	cfg.Memory.ProactiveEnabled = true

	// Should not panic when called with memory disabled.
	e.maybeTriggerProactiveMemoryHook(context.Background(), cfg, nil, "default", "default", "sess_1", "run_1", agent.ToolCallRecord{
		Request: agent.ToolCallRequest{Name: "memory.maintenance"},
		Result:  agent.ToolCallResult{Output: `{"archived_stale_count":1}`},
	})
	// No assertion beyond "doesn't panic" — the function returns early.
}

func TestProactiveHookSkipsWhenProactiveDisabled(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.ProactiveEnabled = false

	// Should not panic when proactive is disabled.
	e.maybeTriggerProactiveMemoryHook(context.Background(), cfg, nil, "default", "default", "sess_1", "run_1", agent.ToolCallRecord{
		Request: agent.ToolCallRequest{Name: "memory.maintenance"},
		Result:  agent.ToolCallResult{Output: `{"archived_stale_count":1}`},
	})
}

func TestProactiveSignalNoTriggerForLowImportanceCheckpoint(t *testing.T) {
	// Checkpoint with low importance items (importance < 4) should not trigger.
	checkpoint := `{"result":{"new_items":[{"kind":"note","content":"minor note","importance":2}]}}`
	sig := proactiveSignalFromToolOutput("memory.checkpoint", checkpoint)
	if sig.Trigger {
		t.Fatalf("expected no trigger for low-importance checkpoint, got %+v", sig)
	}
}

func TestProactiveSignalNoTriggerForMaintenanceWithZeroStale(t *testing.T) {
	maintenance := `{"archived_stale_count":0}`
	sig := proactiveSignalFromToolOutput("memory.maintenance", maintenance)
	if sig.Trigger {
		t.Fatalf("expected no trigger for zero stale count, got %+v", sig)
	}
}

func TestProactiveSignalNoTriggerForEmptyOutput(t *testing.T) {
	sig := proactiveSignalFromToolOutput("memory.checkpoint", "")
	if sig.Trigger {
		t.Fatalf("expected no trigger for empty output, got %+v", sig)
	}
}

func TestProactiveSignalNoTriggerForInvalidJSON(t *testing.T) {
	sig := proactiveSignalFromToolOutput("memory.checkpoint", "not json")
	if sig.Trigger {
		t.Fatalf("expected no trigger for invalid JSON, got %+v", sig)
	}
}
