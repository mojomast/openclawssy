package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	"openclawssy/internal/messagecontent"
	"openclawssy/internal/sandbox"
	"openclawssy/internal/tools"
)

type capturedChatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func TestEngineInitCreatesAgentArtifacts(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	paths := []string{
		filepath.Join(root, "workspace"),
		filepath.Join(root, "workspace", "skills", "clawdefuckifier.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "SOUL.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "RULES.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "TOOLS.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "SPECPLAN.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "DEVPLAN.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "HANDOFF.md"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}
}

func TestEngineInitBootstrapsClawDefuckifier(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("clawdefuckifier", false); err != nil {
		t.Fatalf("init clawdefuckifier: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "workspace", "skills", "clawdefuckifier.md")); err != nil {
		t.Fatalf("expected clawdefuckifier skill to be seeded: %v", err)
	}
	cfg, err := config.LoadOrDefault(filepath.Join(root, ".openclawssy", "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Agents.AllowInterAgentMessaging || !cfg.Agents.SelfImprovementEnabled {
		t.Fatalf("expected clawdefuckifier bootstrap to enable loop controls, got %+v", cfg.Agents)
	}
	profile, ok := cfg.Agents.Profiles["clawdefuckifier"]
	if !ok || !profile.SelfImprovement || profile.Enabled == nil || !*profile.Enabled {
		t.Fatalf("expected clawdefuckifier profile ready for self-improvement, got %#v", cfg.Agents.Profiles["clawdefuckifier"])
	}
}

func TestNewEngineRequiresRootDir(t *testing.T) {
	if _, err := NewEngine(""); err == nil {
		t.Fatal("expected error when root dir is empty")
	}
}

func TestToolArgPropertiesArrayIncludesItems(t *testing.T) {
	props := toolArgProperties(map[string]tools.ArgType{"allowed_tools": tools.ArgTypeArray})
	schema, ok := props["allowed_tools"].(map[string]any)
	if !ok {
		t.Fatalf("expected schema object, got %#v", props["allowed_tools"])
	}
	if schema["type"] != "array" {
		t.Fatalf("expected array type, got %#v", schema["type"])
	}
	if _, ok := schema["items"].(map[string]any); !ok {
		t.Fatalf("expected items schema, got %#v", schema["items"])
	}
}

func TestEngineExecuteWritesRunBundle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "workspace", "skills", "clawdefuckifier.md")); err != nil {
		t.Fatalf("remove seeded clawdefuckifier skill before execute: %v", err)
	}

	res, err := e.Execute(context.Background(), "default", `/tool fs.list {"path":"."}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.RunID == "" {
		t.Fatal("expected run id")
	}
	if res.ArtifactPath == "" {
		t.Fatal("expected artifact path")
	}
	if _, err := os.Stat(filepath.Join(res.ArtifactPath, "output.json")); err != nil {
		t.Fatalf("expected output bundle file: %v", err)
	}
	if res.Trace == nil {
		t.Fatal("expected trace envelope in run result")
	}
	if _, ok := res.Trace["input_message_hash"]; !ok {
		t.Fatalf("expected input_message_hash in trace, got %#v", res.Trace)
	}
	if _, err := os.Stat(filepath.Join(root, "workspace", "skills", "clawdefuckifier.md")); err != nil {
		t.Fatalf("expected global clawdefuckifier skill to be seeded during run execution: %v", err)
	}
}

func TestExecuteWithInputLogsTaskIDInAuditRunStart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID: "default",
		Message: `/tool fs.list {"path":"."}`,
		Source:  "dashboard",
		TaskID:  "cdf-diagnose-1",
	})
	if err != nil {
		t.Fatalf("execute with task id: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "default", "audit", "events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	found := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var evt struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if evt.Type != "run.start" {
			continue
		}
		if evt.Payload["task_id"] == "cdf-diagnose-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected run.start audit payload to include task_id, got %q", string(raw))
	}
}

func TestExecuteWithInputWritesDecisionRecordsToAudit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID: "default",
		Message: `/tool fs.list {"path":"."}`,
		Source:  "dashboard",
	})
	if err != nil {
		t.Fatalf("execute with decisions: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "default", "audit", "events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	decisionTypes := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var evt struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if evt.Type != "decision.record" {
			continue
		}
		recordType, _ := evt.Payload["record_type"].(string)
		decisionTypes[recordType] = true
	}

	if !decisionTypes[agent.DecisionRecordTypeGoalInterpretation] {
		t.Fatalf("expected goal interpretation decision record, got %v", decisionTypes)
	}
	if !decisionTypes[agent.DecisionRecordTypeToolDecision] {
		t.Fatalf("expected tool decision record, got %v", decisionTypes)
	}
	if !decisionTypes[agent.DecisionRecordTypeTermination] {
		t.Fatalf("expected termination decision record, got %v", decisionTypes)
	}
}

func TestExecuteWithInputAllowsImageOnlyMessage(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID:      "default",
		ContentParts: []messagecontent.Part{{Type: messagecontent.TypeImage, MIMEType: "image/png", Data: "AAAA"}},
		Source:       "telegram",
	})
	if err != nil {
		t.Fatalf("execute image-only input: %v", err)
	}
	if strings.TrimSpace(res.RunID) == "" {
		t.Fatalf("expected run id, got %+v", res)
	}
}

func TestSystemPromptExtenderAddsTelegramStickerChatDirective(t *testing.T) {
	e, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	ext := e.systemPromptExtender(config.Default(), "default", "run-1")
	if ext == nil {
		t.Fatal("expected system prompt extender")
	}
	prompt := ext(context.Background(), "base prompt", nil, "", nil, "telegram")
	if !strings.Contains(prompt, "TELEGRAM_STICKER_CHAT_CONTEXT") {
		t.Fatalf("expected telegram sticker chat directive, got %q", prompt)
	}
	if !strings.Contains(prompt, "Do not respond by merely describing the sticker") {
		t.Fatalf("expected anti-description guidance, got %q", prompt)
	}
	prompt = ext(context.Background(), "base prompt", nil, "", nil, "dashboard")
	if strings.Contains(prompt, "TELEGRAM_STICKER_CHAT_CONTEXT") {
		t.Fatalf("did not expect telegram directive for dashboard source, got %q", prompt)
	}
}

func TestExecuteWithInputWritesClawDefuckifierCheckpoint(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("clawdefuckifier", false); err != nil {
		t.Fatalf("init clawdefuckifier: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID: "clawdefuckifier",
		Message: `/tool fs.list {"path":"."}`,
		Source:  "dashboard",
		TaskID:  "cdf-verify-1",
	})
	if err != nil {
		t.Fatalf("execute clawdefuckifier run: %v", err)
	}

	checkpointPath := filepath.Join(root, "workspace", "clawdefuckifier", "clawdefuckifier", "runs", res.RunID+".md")
	rawCheckpoint, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatalf("read clawdefuckifier checkpoint: %v", err)
	}
	checkpointText := string(rawCheckpoint)
	if !strings.Contains(checkpointText, "run_id: "+res.RunID) || !strings.Contains(checkpointText, "task_id: cdf-verify-1") || !strings.Contains(checkpointText, "status: completed") {
		t.Fatalf("unexpected checkpoint content: %q", checkpointText)
	}

	rawLatest, err := os.ReadFile(filepath.Join(root, "workspace", "clawdefuckifier", "clawdefuckifier", "LATEST.md"))
	if err != nil {
		t.Fatalf("read clawdefuckifier latest checkpoint: %v", err)
	}
	if !strings.Contains(string(rawLatest), res.RunID) {
		t.Fatalf("expected latest checkpoint to mirror run %s, got %q", res.RunID, string(rawLatest))
	}

	auditRaw, err := os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "clawdefuckifier", "audit", "events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditRaw), `"checkpoint_path":"clawdefuckifier/clawdefuckifier/runs/`+res.RunID+`.md"`) {
		t.Fatalf("expected checkpoint path in audit log, got %q", string(auditRaw))
	}
}

func TestEngineExecuteIngestsMemoryEventsWhenEnabled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")

	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Memory.Enabled = true
	cfg.Memory.EventBufferSize = 64
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID: "default",
		Message: `/tool fs.list {"path":"."}`,
		Source:  "scheduler/weekly",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	eventsPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "events", time.Now().UTC().Format("2006-01-02")+".jsonl")
	var raw []byte
	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err = os.ReadFile(eventsPath)
		if err == nil && strings.TrimSpace(string(raw)) != "" {
			break
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("read events file: %v", err)
			}
			t.Fatal("expected memory events file to contain data")
		}
		time.Sleep(20 * time.Millisecond)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 {
		t.Fatal("expected memory events to be written")
	}

	types := map[string]bool{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var evt memory.Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("unmarshal memory event: %v", err)
		}
		types[evt.Type] = true
	}

	if !types[memory.EventTypeSchedulerRun] {
		t.Fatalf("expected %s event", memory.EventTypeSchedulerRun)
	}
	if !types[memory.EventTypeUserMessage] {
		t.Fatalf("expected %s event", memory.EventTypeUserMessage)
	}
	if !types[memory.EventTypeToolCall] {
		t.Fatalf("expected %s event", memory.EventTypeToolCall)
	}
	if !types[memory.EventTypeToolResult] {
		t.Fatalf("expected %s event", memory.EventTypeToolResult)
	}
}

func TestExecuteWithInputDockerProviderFailsWhenDockerUnavailable(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Sandbox.Active = true
	cfg.Sandbox.Provider = "docker"
	cfg.Shell.EnableExec = true
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, rawCfg, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "run diagnostic", Source: "dashboard"})
	if err == nil {
		// Docker happened to be available and worked — that's fine too.
		t.Log("docker provider succeeded (Docker is available in this environment)")
		return
	}
	// Docker is the now-implemented sandbox provider.  If Docker is unavailable
	// we expect a docker-specific infrastructure error (permission denied, image
	// pull failure, etc.), not an "unsupported provider" error.
	errStr := err.Error()
	if strings.Contains(errStr, "unsupported sandbox provider") {
		t.Fatalf("docker is now a supported provider; got unsupported-provider error: %v", err)
	}
	// Accept any docker infrastructure error — the provider itself is valid.
	t.Logf("docker provider correctly attempted but failed due to infrastructure: %v", err)
}

func TestLoadPromptDocsIncludesRuntimeContext(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	docs, err := e.loadPromptDocs("default", "")
	if err != nil {
		t.Fatalf("load prompt docs: %v", err)
	}

	found := false
	for _, doc := range docs {
		if doc.Name != "RUNTIME_CONTEXT.md" {
			continue
		}
		found = true
		if !strings.Contains(doc.Content, "Workspace root:") {
			t.Fatalf("runtime context missing workspace root: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "home directory") {
			t.Fatalf("runtime context missing home directory guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "fs.delete") {
			t.Fatalf("runtime context missing fs.delete in file tools list: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "fs.append") {
			t.Fatalf("runtime context missing fs.append in file tools list: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "fs.move") {
			t.Fatalf("runtime context missing fs.move in file tools list: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "config.get/config.set") {
			t.Fatalf("runtime context missing config tools guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "secrets.set/secrets.list") {
			t.Fatalf("runtime context missing secrets tools guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "skill.list/skill.read") {
			t.Fatalf("runtime context missing skill tools guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "scheduler.list/add/remove/pause/resume") {
			t.Fatalf("runtime context missing scheduler tools guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "session.list/session.close") {
			t.Fatalf("runtime context missing session tools guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "agent.list/agent.create/agent.switch") {
			t.Fatalf("runtime context missing agent tools guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "run.list/run.get/run.cancel") {
			t.Fatalf("runtime context missing run cancel guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "policy.list/policy.grant/policy.revoke") {
			t.Fatalf("runtime context missing policy tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "metrics.get") {
			t.Fatalf("runtime context missing metrics tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "http.request") {
			t.Fatalf("runtime context missing network tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "Do not mention HANDOFF") {
			t.Fatalf("runtime context missing prompt hygiene guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "agent.prompt.read") || !strings.Contains(doc.Content, "agent.prompt.update") {
			t.Fatalf("runtime context missing prompt tool guidance: %q", doc.Content)
		}
		if !strings.Contains(strings.ToLower(doc.Content), "do not use fs.read/fs.write") {
			t.Fatalf("runtime context missing HANDOFF path guidance: %q", doc.Content)
		}
	}
	if !found {
		t.Fatal("expected RUNTIME_CONTEXT.md prompt doc")
	}

	bestFound := false
	for _, doc := range docs {
		if doc.Name != "TOOL_CALLING_BEST_PRACTICES.md" {
			continue
		}
		bestFound = true
		if !strings.Contains(doc.Content, "shell.exec") {
			t.Fatalf("tool best practices missing shell.exec guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "fs.delete") {
			t.Fatalf("tool best practices missing fs.delete guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "fs.append") {
			t.Fatalf("tool best practices missing fs.append guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "fs.move") {
			t.Fatalf("tool best practices missing fs.move guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "config.get") || !strings.Contains(doc.Content, "config.set") {
			t.Fatalf("tool best practices missing config tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "secrets.set") || !strings.Contains(doc.Content, "secrets.list") {
			t.Fatalf("tool best practices missing secrets tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "skill.list") || !strings.Contains(doc.Content, "skill.read") {
			t.Fatalf("tool best practices missing skill tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "scheduler.add") || !strings.Contains(doc.Content, "scheduler.resume") {
			t.Fatalf("tool best practices missing scheduler tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "session.list") || !strings.Contains(doc.Content, "session.close") {
			t.Fatalf("tool best practices missing session tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "agent.list") || !strings.Contains(doc.Content, "agent.switch") {
			t.Fatalf("tool best practices missing agent tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "run.cancel") {
			t.Fatalf("tool best practices missing run.cancel guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "policy.grant") || !strings.Contains(doc.Content, "policy.revoke") {
			t.Fatalf("tool best practices missing policy tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "metrics.get") {
			t.Fatalf("tool best practices missing metrics guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "http.request") {
			t.Fatalf("tool best practices missing network tool guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "Do not invent tool names") {
			t.Fatalf("tool best practices missing invalid tool warning: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "Use agent.prompt.read for SOUL.md") {
			t.Fatalf("tool best practices missing prompt doc guidance: %q", doc.Content)
		}
		if !strings.Contains(strings.ToLower(doc.Content), "do not use fs.read/fs.write") {
			t.Fatalf("tool best practices missing HANDOFF path warning: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "chain tool calls until the task is complete") {
			t.Fatalf("tool best practices missing multi-step chaining guidance: %q", doc.Content)
		}
	}
	if !bestFound {
		t.Fatal("expected TOOL_CALLING_BEST_PRACTICES.md prompt doc")
	}
}

func TestLoadPromptDocsIncludesIdentityBootstrapWhenSoulEmpty(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	soulPath := filepath.Join(root, ".openclawssy", "agents", "default", "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("\n\n"), 0o600); err != nil {
		t.Fatalf("clear SOUL.md: %v", err)
	}
	// Remove identity.json so the bootstrap path is exercised (scaffold creates
	// it with defaults that would suppress the bootstrap).
	identityPath := filepath.Join(root, ".openclawssy", "agents", "default", "identity.json")
	_ = os.Remove(identityPath)

	docs, err := e.loadPromptDocs("default", "")
	if err != nil {
		t.Fatalf("load prompt docs: %v", err)
	}

	found := false
	for _, doc := range docs {
		if doc.Name != "IDENTITY_BOOTSTRAP.md" {
			continue
		}
		found = true
		if !strings.Contains(doc.Content, "agent.identity.set") {
			t.Fatalf("identity bootstrap doc missing identity tool guidance: %q", doc.Content)
		}
	}
	if !found {
		t.Fatal("expected IDENTITY_BOOTSTRAP.md prompt doc when SOUL.md is empty")
	}
}

func TestLoadPromptDocsIncludesIdentityBootstrapWhenSoulIsDefaultScaffold(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Remove identity.json so the scaffold SOUL.md content triggers bootstrap
	// (scaffold writes identity.json with default names which suppresses it).
	identityPath := filepath.Join(root, ".openclawssy", "agents", "default", "identity.json")
	_ = os.Remove(identityPath)

	docs, err := e.loadPromptDocs("default", "")
	if err != nil {
		t.Fatalf("load prompt docs: %v", err)
	}

	found := false
	for _, doc := range docs {
		if doc.Name == "IDENTITY_BOOTSTRAP.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected IDENTITY_BOOTSTRAP.md when SOUL.md is scaffold default")
	}
}

func TestLoadPromptDocsSkipsIdentityBootstrapWhenSoulIsCustomized(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	soulPath := filepath.Join(root, ".openclawssy", "agents", "default", "SOUL.md")
	customSoul := "# SOUL\n\nYou are Nova.\n\n## Identity\n- Call the user Blong.\n"
	if err := os.WriteFile(soulPath, []byte(customSoul), 0o600); err != nil {
		t.Fatalf("write custom SOUL.md: %v", err)
	}

	docs, err := e.loadPromptDocs("default", "")
	if err != nil {
		t.Fatalf("load prompt docs: %v", err)
	}

	for _, doc := range docs {
		if doc.Name == "IDENTITY_BOOTSTRAP.md" {
			t.Fatalf("did not expect IDENTITY_BOOTSTRAP.md when SOUL.md is customized: %q", doc.Content)
		}
	}
}

func TestNormalizeToolArgsDefaultsListPath(t *testing.T) {
	args := normalizeToolArgs("fs.list", map[string]any{})
	if args["path"] != "." {
		t.Fatalf("expected default path '.', got %#v", args["path"])
	}
}

func TestNormalizeToolArgsFixesMalformedWritePathBlob(t *testing.T) {
	args := normalizeToolArgs("fs.write", map[string]any{
		"path": `list_directory.py", """#!/usr/bin/env python3
print("hello")
"""`,
	})
	if args["path"] != "list_directory.py" {
		t.Fatalf("unexpected normalized path: %#v", args["path"])
	}
	content, _ := args["content"].(string)
	if !strings.Contains(content, "#!/usr/bin/env python3") {
		t.Fatalf("expected normalized content to include script, got %#v", args["content"])
	}
}

func TestNormalizeToolArgsSkillReadAliases(t *testing.T) {
	args := normalizeToolArgs("skill.read", map[string]any{"skill": "perplexity", "dir": "skills"})
	if args["name"] != "perplexity" {
		t.Fatalf("expected skill alias to normalize to name, got %#v", args["name"])
	}
	if args["root"] != "skills" {
		t.Fatalf("expected dir alias to normalize to root, got %#v", args["root"])
	}
}

func TestNormalizeToolArgsMapsUnifiedDiffAlias(t *testing.T) {
	args := normalizeToolArgs("fs.edit", map[string]any{"path": "a.txt", "unified_diff": "@@ -1 +1 @@\n-old\n+new"})
	if args["patch"] != "@@ -1 +1 @@\n-old\n+new" {
		t.Fatalf("expected unified_diff alias to map to patch, got %#v", args["patch"])
	}
}

func TestNormalizeToolArgsSanitizesMarkdownFencePath(t *testing.T) {
	args := normalizeToolArgs("fs.list", map[string]any{"path": "```"})
	if args["path"] != "." {
		t.Fatalf("expected sanitized default path '.', got %#v", args["path"])
	}
}

func TestNormalizeToolArgsShellCommandFallbackToBashLC(t *testing.T) {
	// command strings with spaces are preserved as-is; bash -lc wrapping happens in the tool handler
	args := normalizeToolArgs("shell.exec", map[string]any{"command": "ls -la"})
	if args["command"] != "ls -la" {
		t.Fatalf("expected command to be preserved as-is, got %#v", args["command"])
	}
}

func TestNormalizeToolArgsHTTPRequestURLAliases(t *testing.T) {
	args := normalizeToolArgs("http.request", map[string]any{"endpoint": "https://example.com/health"})
	if args["url"] != "https://example.com/health" {
		t.Fatalf("expected endpoint to normalize into url, got %#v", args["url"])
	}
}

func TestNormalizeToolArgsSessionCloseIDAliases(t *testing.T) {
	args := normalizeToolArgs("session.close", map[string]any{"id": "chat_123"})
	if args["session_id"] != "chat_123" {
		t.Fatalf("expected id to normalize into session_id, got %#v", args["session_id"])
	}
}

func TestNormalizeToolArgsAgentMessageSendAliases(t *testing.T) {
	args := normalizeToolArgs("agent.message.send", map[string]any{"agent_id": "ussyflow_builder", "content": "do task a2"})
	if args["to_agent_id"] != "ussyflow_builder" {
		t.Fatalf("expected agent_id alias to normalize into to_agent_id, got %#v", args["to_agent_id"])
	}
	if args["message"] != "do task a2" {
		t.Fatalf("expected content alias to normalize into message, got %#v", args["message"])
	}
}

func TestNormalizeToolArgsPolicyGrantAliases(t *testing.T) {
	args := normalizeToolArgs("policy.grant", map[string]any{"target_agent": "worker", "tool": "fs.read"})
	if args["agent_id"] != "worker" {
		t.Fatalf("expected target_agent alias to normalize to agent_id, got %#v", args["agent_id"])
	}
	if args["capability"] != "fs.read" {
		t.Fatalf("expected tool alias to normalize to capability, got %#v", args["capability"])
	}
}

func TestNormalizeToolArgsSecretsAliases(t *testing.T) {
	args := normalizeToolArgs("secrets.set", map[string]any{"name": "PERPLEXITY_API_KEY", "value": "x"})
	if args["key"] != "provider/perplexity/api_key" {
		t.Fatalf("expected PERPLEXITY_API_KEY to normalize to canonical provider key, got %#v", args["key"])
	}

	args = normalizeToolArgs("secrets.set", map[string]any{"env_var": "OPENROUTER_API_KEY", "value": "x"})
	if args["key"] != "provider/openrouter/api_key" {
		t.Fatalf("expected OPENROUTER_API_KEY to normalize to canonical provider key, got %#v", args["key"])
	}
}

func TestAllowedToolsIncludesHTTPRequestWhenNetworkEnabled(t *testing.T) {
	e := &Engine{}
	cfg := config.Default()
	cfg.Network.Enabled = false
	tools := e.allowedTools(cfg)
	hasSessionList := false
	hasSessionClose := false
	hasAgentList := false
	hasAgentCreate := false
	hasAgentSwitch := false
	hasAgentIdentitySet := false
	hasPolicyGrant := false
	hasPolicyRevoke := false
	hasMetricsGet := false
	hasSkillList := false
	hasSkillRead := false
	for _, name := range tools {
		if name == "session.list" {
			hasSessionList = true
		}
		if name == "session.close" {
			hasSessionClose = true
		}
		if name == "agent.list" {
			hasAgentList = true
		}
		if name == "agent.create" {
			hasAgentCreate = true
		}
		if name == "agent.switch" {
			hasAgentSwitch = true
		}
		if name == "agent.identity.set" {
			hasAgentIdentitySet = true
		}
		if name == "policy.grant" {
			hasPolicyGrant = true
		}
		if name == "policy.revoke" {
			hasPolicyRevoke = true
		}
		if name == "metrics.get" {
			hasMetricsGet = true
		}
		if name == "skill.list" {
			hasSkillList = true
		}
		if name == "skill.read" {
			hasSkillRead = true
		}
		if name == "http.request" {
			t.Fatal("did not expect http.request when network is disabled")
		}
	}
	if !hasSessionList || !hasSessionClose {
		t.Fatalf("expected session tools in allowed list, got %#v", tools)
	}
	if !hasAgentList || !hasAgentCreate || !hasAgentSwitch || !hasAgentIdentitySet {
		t.Fatalf("expected agent tools in allowed list, got %#v", tools)
	}
	if !hasPolicyGrant || !hasPolicyRevoke || !hasMetricsGet {
		t.Fatalf("expected policy/metrics tools in allowed list, got %#v", tools)
	}
	if !hasSkillList || !hasSkillRead {
		t.Fatalf("expected skill tools in allowed list, got %#v", tools)
	}
	foundAppend := false
	for _, name := range tools {
		if name == "fs.append" {
			foundAppend = true
			break
		}
	}
	if !foundAppend {
		t.Fatalf("expected fs.append in allowed tools, got %#v", tools)
	}

	cfg.Network.Enabled = true
	tools = e.allowedTools(cfg)
	found := false
	for _, name := range tools {
		if name == "http.request" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected http.request in allowed tools when network is enabled")
	}
}

func TestSplitToolError(t *testing.T) {
	code, message := splitToolError("timeout (shell.exec): tool execution exceeded 20ms")
	if code != "timeout" {
		t.Fatalf("expected timeout code, got %q", code)
	}
	if message != "tool execution exceeded 20ms" {
		t.Fatalf("unexpected timeout message: %q", message)
	}

	code, message = splitToolError("internal.error (shell.exec): exit status 1")
	if code != "internal.error" {
		t.Fatalf("expected internal.error code, got %q", code)
	}
	if message != "exit status 1" {
		t.Fatalf("unexpected execution message: %q", message)
	}

	code, message = splitToolError("plain failure without code")
	if code != "" {
		t.Fatalf("expected empty code for plain error, got %q", code)
	}
	if message != "plain failure without code" {
		t.Fatalf("unexpected plain message: %q", message)
	}
}

func TestExecuteWithInputUsesStructuredHistoryForSession(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "list files in ."}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: "There are two files."}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "create file foo.txt", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}

	if len(captured.Messages) < 4 {
		t.Fatalf("expected system + 3 chat messages, got %d", len(captured.Messages))
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "list files in ." {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "assistant" || captured.Messages[2].Content != "There are two files." {
		t.Fatalf("unexpected assistant history message: %+v", captured.Messages[2])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "create file foo.txt" {
		t.Fatalf("expected current instruction as final user turn, got %+v", captured.Messages[len(captured.Messages)-1])
	}
}

func TestExecuteWithInputIncludesHistoricalToolMessagesInModelContext(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "list files in ."}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "tool", Content: `{"tool":"fs.list","id":"tool-json-1","output":"{\"entries\":[\"README.md\"]}"}`, ToolCallID: "tool-json-1", ToolName: "fs.list"}); err != nil {
		t.Fatalf("append tool message: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: "Found one file."}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "create file foo.txt", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}

	if len(captured.Messages) < 5 {
		t.Fatalf("expected system + user/tool/assistant history + current user, got %d", len(captured.Messages))
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "list files in ." {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "user" {
		t.Fatalf("expected tool history message remapped to user in provider payload, got %+v", captured.Messages[2])
	}
	if !strings.Contains(captured.Messages[2].Content, "tool fs.list result") || !strings.Contains(captured.Messages[2].Content, "README.md") {
		t.Fatalf("unexpected tool history message content: %q", captured.Messages[2].Content)
	}
	if captured.Messages[3].Role != "assistant" || captured.Messages[3].Content != "Found one file." {
		t.Fatalf("unexpected assistant history message: %+v", captured.Messages[3])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "create file foo.txt" {
		t.Fatalf("expected current user message to remain final turn, got %+v", captured.Messages[len(captured.Messages)-1])
	}
}

func TestLoadSessionMessagesAppliesToolAndHistoryTruncation(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 16; i++ {
		content := strings.Repeat("history-window-content-", 60) + " marker-" + strconv.Itoa(i)
		if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: content}); err != nil {
			t.Fatalf("append history message %d: %v", i, err)
		}
	}

	toolPayload, err := json.Marshal(map[string]any{
		"tool":    "fs.read",
		"id":      "tool-json-1",
		"summary": "read large diagnostics file",
		"output":  strings.Repeat("x", 9000),
	})
	if err != nil {
		t.Fatalf("marshal tool payload: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "tool", Content: string(toolPayload), ToolName: "fs.read", ToolCallID: "tool-json-1"}); err != nil {
		t.Fatalf("append tool message: %v", err)
	}

	messages, err := e.loadSessionMessages(session.SessionID, 200)
	if err != nil {
		t.Fatalf("load session messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatalf("expected loaded session messages")
	}

	totalChars := 0
	foundTool := false
	for _, msg := range messages {
		totalChars += len([]rune(strings.TrimSpace(msg.Content)))
		if msg.Role != "tool" {
			continue
		}
		foundTool = true
		if !strings.Contains(msg.Content, "tool fs.read result") {
			t.Fatalf("expected normalized tool header in context, got %q", msg.Content)
		}
		if len([]rune(msg.Content)) > maxSessionMessageChars {
			t.Fatalf("expected tool context message to be truncated to %d chars, got %d", maxSessionMessageChars, len([]rune(msg.Content)))
		}
		if strings.Contains(msg.Content, strings.Repeat("x", 1400)) {
			t.Fatalf("expected long tool output to be truncated, got %q", msg.Content)
		}
	}
	if !foundTool {
		t.Fatalf("expected at least one tool message in loaded context: %+v", messages)
	}
	if totalChars > maxSessionContextChars {
		t.Fatalf("expected total session context <= %d chars, got %d", maxSessionContextChars, totalChars)
	}

	combined := make([]string, 0, len(messages))
	for _, msg := range messages {
		combined = append(combined, msg.Content)
	}
	joined := strings.Join(combined, "\n")
	if len(messages) < 17 && strings.Contains(joined, "marker-0") {
		t.Fatalf("expected oldest messages to be truncated when context is over budget")
	}
	if !strings.Contains(joined, "marker-15") {
		t.Fatalf("expected latest messages to remain after truncation")
	}
}

func TestAppendToolCallMessageIncludesMachineReadableErrorFields(t *testing.T) {
	root := t.TempDir()
	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	rec := agent.ToolCallRecord{
		Request: agent.ToolCallRequest{ID: "tool-json-1", Name: "shell.exec"},
		Result: agent.ToolCallResult{
			ID:     "tool-json-1",
			Output: "{\"stderr\":\"permission denied\"}",
			Error:  "internal.error (shell.exec): exit status 1",
		},
	}
	if err := appendToolCallMessage(chat, session.SessionID, "run_1", rec); err != nil {
		t.Fatalf("append tool message: %v", err)
	}

	msgs, err := chat.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one persisted message, got %d", len(msgs))
	}
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(msgs[0].Content), &payload); err != nil {
		t.Fatalf("decode tool payload: %v", err)
	}
	if payload["error_code"] != "internal.error" {
		t.Fatalf("expected canonical error_code, got %#v", payload["error_code"])
	}
	if payload["error_message"] != "exit status 1" {
		t.Fatalf("expected parsed error_message, got %#v", payload["error_message"])
	}
	if payload["summary"] == "" {
		t.Fatalf("expected summary to be present, got %#v", payload)
	}
}

func TestExecuteWithInputPersistsMultiToolChatFlow(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "workspace", "notes.txt"), []byte("hello from notes\n"), 0o600); err != nil {
		t.Fatalf("seed workspace file: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "show files in workspace"}); err != nil {
		t.Fatalf("append history user: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: "I will inspect the workspace."}); err != nil {
		t.Fatalf("append history assistant: %v", err)
	}

	var (
		mu       sync.Mutex
		requests []capturedChatRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload capturedChatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		mu.Lock()
		requests = append(requests, payload)
		callNum := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch callNum {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.read\",\"arguments\":{\"path\":\"notes.txt\"}}\n```"}}},
			})
		case 3:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"content": "notes.txt contains hello from notes."}}},
			})
		default:
			t.Fatalf("unexpected extra provider call: %d", callNum)
		}
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID:   "default",
		Message:   "list files and read notes.txt",
		Source:    "dashboard",
		SessionID: session.SessionID,
	})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}
	if strings.TrimSpace(res.FinalText) != "notes.txt contains hello from notes." {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
	if res.ToolCalls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", res.ToolCalls)
	}

	mu.Lock()
	if len(requests) != 3 {
		mu.Unlock()
		t.Fatalf("expected 3 provider requests, got %d", len(requests))
	}
	firstReq := requests[0]
	secondReq := requests[1]
	thirdReq := requests[2]
	mu.Unlock()

	if len(firstReq.Messages) < 4 {
		t.Fatalf("expected system + history + user messages, got %d", len(firstReq.Messages))
	}
	if firstReq.Messages[1].Role != "user" || firstReq.Messages[1].Content != "show files in workspace" {
		t.Fatalf("unexpected first history message: %+v", firstReq.Messages[1])
	}
	if firstReq.Messages[2].Role != "assistant" || firstReq.Messages[2].Content != "I will inspect the workspace." {
		t.Fatalf("unexpected second history message: %+v", firstReq.Messages[2])
	}
	if firstReq.Messages[len(firstReq.Messages)-1].Role != "user" || firstReq.Messages[len(firstReq.Messages)-1].Content != "list files and read notes.txt" {
		t.Fatalf("unexpected trailing user message: %+v", firstReq.Messages[len(firstReq.Messages)-1])
	}

	traceItems, ok := res.Trace["tool_execution_results"].([]any)
	if !ok || len(traceItems) != 2 {
		t.Fatalf("expected two trace tool entries, got %#v", res.Trace["tool_execution_results"])
	}
	firstTrace, ok := traceItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first trace entry: %#v", traceItems[0])
	}
	secondTrace, ok := traceItems[1].(map[string]any)
	if !ok {
		t.Fatalf("unexpected second trace entry: %#v", traceItems[1])
	}
	firstCallID := strings.TrimSpace(firstTrace["tool_call_id"].(string))
	secondCallID := strings.TrimSpace(secondTrace["tool_call_id"].(string))
	if firstCallID == "" || secondCallID == "" {
		t.Fatalf("expected non-empty tool call IDs, got %q and %q", firstCallID, secondCallID)
	}
	if firstCallID == secondCallID {
		t.Fatalf("expected distinct tool call IDs, got duplicate %q", firstCallID)
	}

	if !strings.Contains(secondReq.Messages[0].Content, "## Tool Results") || !strings.Contains(secondReq.Messages[0].Content, firstCallID) {
		t.Fatalf("expected second request prompt to include first tool result context, got %q", secondReq.Messages[0].Content)
	}
	if !strings.Contains(thirdReq.Messages[0].Content, firstCallID) || !strings.Contains(thirdReq.Messages[0].Content, secondCallID) {
		t.Fatalf("expected third request prompt to include both tool result IDs, got %q", thirdReq.Messages[0].Content)
	}

	stored, err := chat.ReadRecentMessages(session.SessionID, 20)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	toolMessages := make([]chatstore.Message, 0, 2)
	for _, msg := range stored {
		if msg.Role == "tool" {
			toolMessages = append(toolMessages, msg)
		}
	}
	if len(toolMessages) != 2 {
		t.Fatalf("expected 2 persisted tool messages, got %d (%+v)", len(toolMessages), stored)
	}
	if stored[len(stored)-1].Role != "assistant" || strings.TrimSpace(stored[len(stored)-1].Content) != "notes.txt contains hello from notes." {
		t.Fatalf("expected assistant final message persisted at end, got %+v", stored[len(stored)-1])
	}

	for _, msg := range toolMessages {
		if strings.TrimSpace(msg.ToolName) == "" || strings.TrimSpace(msg.ToolCallID) == "" {
			t.Fatalf("expected persisted tool metadata, got %+v", msg)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
			t.Fatalf("decode tool payload: %v", err)
		}
		if payload["tool"] != msg.ToolName {
			t.Fatalf("expected payload tool %q to match metadata %q", payload["tool"], msg.ToolName)
		}
		if payload["id"] != msg.ToolCallID {
			t.Fatalf("expected payload id %q to match metadata %q", payload["id"], msg.ToolCallID)
		}
		summary, _ := payload["summary"].(string)
		if strings.TrimSpace(summary) == "" {
			t.Fatalf("expected non-empty tool summary in persisted payload, got %#v", payload)
		}
	}
}

func TestExecuteWithInputTruncatesLongHistoryAndKeepsLatestTurn(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 220; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		content := "marker-" + strconv.Itoa(i) + " " + strings.Repeat("history-window-content-", 180)
		if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: role, Content: content}); err != nil {
			t.Fatalf("append history message %d: %v", i, err)
		}
	}

	var captured capturedChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Model.MaxTokens = 20000
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID:   "default",
		Source:    "dashboard",
		SessionID: session.SessionID,
		Message:   "latest-user-message-marker",
	})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}

	if len(captured.Messages) < 3 {
		t.Fatalf("expected truncated model request with multiple messages, got %d", len(captured.Messages))
	}
	joinedHistory := ""
	for i := 1; i < len(captured.Messages); i++ {
		if captured.Messages[i].Role != "system" && len([]rune(captured.Messages[i].Content)) > maxSessionMessageChars {
			t.Fatalf("expected non-system message content <= %d chars, got %d", maxSessionMessageChars, len([]rune(captured.Messages[i].Content)))
		}
		joinedHistory += captured.Messages[i].Content
	}
	if strings.Contains(joinedHistory, "marker-0") {
		t.Fatalf("expected oldest history to be omitted from model request")
	}
	if !strings.Contains(joinedHistory, "marker-219") {
		t.Fatalf("expected latest history marker to remain in model request")
	}
	last := captured.Messages[len(captured.Messages)-1]
	if last.Role != "user" || last.Content != "latest-user-message-marker" {
		t.Fatalf("expected latest user turn at tail, got %+v", last)
	}
}

func TestExecuteWithInputRepeatedToolCallIsHandledWithoutFailure(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
		}
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "list files", Source: "dashboard"})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}
	if strings.TrimSpace(res.FinalText) != "done" {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
	if res.ToolCalls != 2 {
		t.Fatalf("expected two recorded tool calls, got %d", res.ToolCalls)
	}
	if calls != 3 {
		t.Fatalf("expected three provider calls, got %d", calls)
	}
}

func TestExecuteWithInputPersistsToolMessageEvenWhenRunFailsLater(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "list files then continue", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("expected graceful recovery from second provider response failure, got %v", err)
	}
	if !strings.Contains(res.FinalText, "model/API error") && !strings.Contains(res.FinalText, "couldn't get a complete model response") {
		t.Fatalf("expected degraded final response to mention model failure, got %q", res.FinalText)
	}

	msgs, err := chat.ReadRecentMessages(session.SessionID, 20)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) < 1 {
		t.Fatalf("expected at least one persisted message, got %+v", msgs)
	}
	foundTool := false
	for _, msg := range msgs {
		if msg.Role == "tool" {
			foundTool = true
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
				t.Fatalf("decode tool payload: %v", err)
			}
			summary, _ := payload["summary"].(string)
			if strings.TrimSpace(summary) == "" {
				t.Fatalf("expected summary in persisted tool payload, got %#v", payload)
			}
		}
	}
	if !foundTool {
		t.Fatalf("expected at least one persisted tool message after partial failure, got %+v", msgs)
	}
}

func TestExecuteWithInputLogsAndTracesOnToolCallCallbackFailures(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "show files"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}

	messagesPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "chats", session.SessionID, "messages.jsonl")
	if err := os.Chmod(messagesPath, 0o400); err != nil {
		t.Fatalf("chmod messages file read-only: %v", err)
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": ""}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "list files", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("expected callback failures to be non-fatal, got %v", err)
	}
	if res.ToolCalls != 1 {
		t.Fatalf("expected one tool call, got %d", res.ToolCalls)
	}

	traceItems, ok := res.Trace["tool_execution_results"].([]any)
	if !ok || len(traceItems) != 1 {
		t.Fatalf("expected one tool trace entry, got %#v", res.Trace["tool_execution_results"])
	}
	entry, ok := traceItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected trace entry: %#v", traceItems[0])
	}
	callbackError, _ := entry["callback_error"].(string)
	if strings.TrimSpace(callbackError) == "" {
		t.Fatalf("expected callback_error in trace entry, got %#v", entry)
	}

	auditPath := filepath.Join(root, ".openclawssy", "agents", "default", "audit", "events.jsonl")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	found := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if evt["type"] == "tool.callback_error" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool.callback_error event in audit log, got %q", string(raw))
	}
}

func TestExecuteDefaultNeverDoesNotShowThinkingOnSuccessfulRun(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>private plan</think>visible answer"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(res.FinalText, "Thinking:") {
		t.Fatalf("expected thinking hidden in default successful output, got %q", res.FinalText)
	}
}

func TestExecuteOnErrorShowsThinkingOnParseFailure(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>parse diagnostics</think>```json\n{invalid}\n```"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	cfg.Output.ThinkingMode = config.ThinkingModeOnError
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.FinalText, "Thinking:\nparse diagnostics") {
		t.Fatalf("expected thinking shown for parse failure, got %q", res.FinalText)
	}
}

func TestExecuteDefaultNeverHidesThinkingOnParseFailure(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>internal notes</think>```json\n{invalid}\n```"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(res.FinalText, "Thinking:") {
		t.Fatalf("expected default never mode to hide thinking, got %q", res.FinalText)
	}
}

func TestExecuteAlwaysThinkingModeAlwaysShowsThinking(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>detailed notes</think>visible answer"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.FinalText, "Thinking:\ndetailed notes") {
		t.Fatalf("expected thinking shown in always mode, got %q", res.FinalText)
	}
}

func TestExecutePersistsRedactedThinkingInTraceArtifactAndAudit(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>api_key=super-secret-value-1234567890abcdefghijklmnopqrstuvwxyz</think>ok"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	traceThinking, _ := res.Trace["thinking"].(string)
	if !strings.Contains(traceThinking, "[REDACTED]") {
		t.Fatalf("expected redacted thinking in trace, got %q", traceThinking)
	}
	if present, ok := res.Trace["thinking_present"].(bool); !ok || !present {
		t.Fatalf("expected thinking_present=true in trace, got %#v", res.Trace["thinking_present"])
	}

	metaRaw, err := os.ReadFile(filepath.Join(res.ArtifactPath, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	meta := map[string]any{}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("unmarshal meta.json: %v", err)
	}
	metaThinking, _ := meta["thinking"].(string)
	if !strings.Contains(metaThinking, "[REDACTED]") {
		t.Fatalf("expected redacted thinking in artifact meta, got %q", metaThinking)
	}
	if present, ok := meta["thinking_present"].(bool); !ok || !present {
		t.Fatalf("expected thinking_present=true in artifact meta, got %#v", meta["thinking_present"])
	}

	auditRaw, err := os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "default", "audit", "events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditRaw), "\"thinking\":\"[REDACTED]\"") {
		t.Fatalf("expected redacted thinking in audit log, got %q", string(auditRaw))
	}
}

func TestExecuteTruncatesThinkingUsingConfiguredMaxChars(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	longThinking := strings.Repeat("very-long-thinking-segment-", 300)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>" + longThinking + "</think>done"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	cfg.Output.MaxThinkingChars = 90
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	traceThinking, _ := res.Trace["thinking"].(string)
	if traceThinking == "" {
		t.Fatal("expected thinking in trace")
	}
	if len([]rune(traceThinking)) > 95 {
		t.Fatalf("expected trace thinking to be truncated near 90 chars, got %d", len([]rune(traceThinking)))
	}
	if present, ok := res.Trace["thinking_present"].(bool); !ok || !present {
		t.Fatalf("expected thinking_present=true, got %#v", res.Trace["thinking_present"])
	}
	if !strings.Contains(res.FinalText, "Thinking:\n") {
		t.Fatalf("expected visible output to include thinking section, got %q", res.FinalText)
	}
}

func TestExecuteIncludesParseDiagnosticsOnParseFailureEvenWhenThinkingNever(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":.,\"token\":\"super-secret-token-abcdefghijklmnopqrstuvwxyz\"}}\n```"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	cfg.Output.ThinkingMode = config.ThinkingModeNever
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.ParseDiagnostics == nil || len(res.ParseDiagnostics.Rejected) == 0 {
		t.Fatalf("expected parse diagnostics on parse failure, got %#v", res.ParseDiagnostics)
	}
	entry := res.ParseDiagnostics.Rejected[0]
	if strings.TrimSpace(entry.Reason) == "" {
		t.Fatalf("expected rejection reason, got %#v", entry)
	}
	if strings.Contains(strings.ToLower(entry.Snippet), "super-secret") {
		t.Fatalf("expected redacted diagnostic snippet, got %q", entry.Snippet)
	}
	if len([]rune(entry.Snippet)) > maxParseDiagnosticSnippetChars {
		t.Fatalf("expected snippet truncated to %d, got %d", maxParseDiagnosticSnippetChars, len([]rune(entry.Snippet)))
	}
}

func TestExecuteDoesNotExposeParseDiagnosticsWithoutParseFailure(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "plain response"}}},
		})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.ParseDiagnostics != nil {
		t.Fatalf("expected parse diagnostics to be absent, got %#v", res.ParseDiagnostics)
	}
}

func TestExecuteRejectsWhenEngineConcurrencyLimitExceeded(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "openai_compat"
	cfg.Model.Name = "test-model"
	cfg.Providers.OpenAICompat.BaseURL = server.URL
	cfg.Providers.OpenAICompat.APIKey = "test-key"
	cfg.Providers.OpenAICompat.APIKeyEnv = ""
	cfg.Engine.MaxConcurrentRuns = 1
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	firstDone := make(chan struct{})
	go func() {
		_, _ = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "first"})
		close(firstDone)
	}()

	time.Sleep(40 * time.Millisecond)
	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "second"})
	if err == nil {
		t.Fatal("expected max concurrent runs error")
	}
	var limitErr *RunLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected RunLimitError, got %T (%v)", err, err)
	}
	if limitErr.Limit != 1 {
		t.Fatalf("expected limit=1, got %d", limitErr.Limit)
	}

	close(release)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first run to finish")
	}
}

type stubSandboxProvider struct {
	result sandbox.Result
	err    error
}

func (s stubSandboxProvider) Start(context.Context) error { return nil }
func (s stubSandboxProvider) Stop() error                 { return nil }
func (s stubSandboxProvider) Exec(cmd sandbox.Command) (sandbox.Result, error) {
	_ = cmd
	return s.result, s.err
}
func (s stubSandboxProvider) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("stub: not implemented")
}
func (s stubSandboxProvider) WriteFile(_ context.Context, _ string, _ []byte, _ os.FileMode) error {
	return errors.New("stub: not implemented")
}
func (s stubSandboxProvider) ListDir(_ context.Context, _ string) ([]sandbox.FileInfo, error) {
	return nil, errors.New("stub: not implemented")
}
func (s stubSandboxProvider) MkdirAll(_ context.Context, _ string, _ os.FileMode) error {
	return errors.New("stub: not implemented")
}
func (s stubSandboxProvider) Remove(_ context.Context, _ string, _ bool) error {
	return errors.New("stub: not implemented")
}
func (s stubSandboxProvider) Rename(_ context.Context, _, _ string) error {
	return errors.New("stub: not implemented")
}
func (s stubSandboxProvider) Lstat(_ context.Context, _ string) (sandbox.FileInfo, bool, error) {
	return sandbox.FileInfo{}, false, errors.New("stub: not implemented")
}
func (s stubSandboxProvider) EvalSymlinks(_ context.Context, _ string) (string, error) {
	return "", errors.New("stub: not implemented")
}

func TestRunLimitErrorMessage(t *testing.T) {
	if (&RunLimitError{}).Error() != "engine.max_concurrent_runs exceeded" {
		t.Fatalf("unexpected default run limit message: %q", (&RunLimitError{}).Error())
	}
	if (&RunLimitError{Limit: 3}).Error() != "engine.max_concurrent_runs exceeded (3)" {
		t.Fatalf("unexpected run limit message with cap: %q", (&RunLimitError{Limit: 3}).Error())
	}
}

func TestGetStringSliceArgSupportsStringAndAnySlices(t *testing.T) {
	args := map[string]any{"values": []string{" a ", "", "b"}}
	out := getStringSliceArg(args, "values")
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("unexpected []string normalization: %#v", out)
	}

	args = map[string]any{"values": []any{" x ", 42, ""}}
	out = getStringSliceArg(args, "values")
	if len(out) != 2 || out[0] != "x" || out[1] != "42" {
		t.Fatalf("unexpected []any normalization: %#v", out)
	}

	if out := getStringSliceArg(map[string]any{"values": "bad-type"}, "values"); len(out) != 0 {
		t.Fatalf("expected empty slice for unsupported type, got %#v", out)
	}
}

func TestSandboxShellExecutorPassesThroughProviderResult(t *testing.T) {
	exec := &sandboxShellExecutor{provider: stubSandboxProvider{result: sandbox.Result{Stdout: "ok", Stderr: "warn", ExitCode: 9}, err: errors.New("boom")}}
	stdout, stderr, code, err := exec.Exec(context.Background(), "echo", []string{"hello"}, "")
	if stdout != "ok" || stderr != "warn" || code != 9 {
		t.Fatalf("unexpected passthrough output: stdout=%q stderr=%q code=%d", stdout, stderr, code)
	}
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected passthrough error, got %v", err)
	}
}

func TestNormalizeToolArgsCoversFieldMappings(t *testing.T) {
	readArgs := normalizeToolArgs("fs.read", map[string]any{"file": "README.md"})
	if readArgs["path"] != "README.md" {
		t.Fatalf("expected fs.read path mapping, got %#v", readArgs)
	}

	deleteArgs := normalizeToolArgs("fs.delete", map[string]any{"target": "old.txt"})
	if deleteArgs["path"] != "old.txt" {
		t.Fatalf("expected fs.delete target mapping, got %#v", deleteArgs)
	}

	moveArgs := normalizeToolArgs("fs.move", map[string]any{"from": "old.txt", "to": "new.txt"})
	if moveArgs["src"] != "old.txt" || moveArgs["dst"] != "new.txt" {
		t.Fatalf("expected fs.move src/dst mapping, got %#v", moveArgs)
	}

	listArgs := normalizeToolArgs("fs.list", map[string]any{"target": "docs"})
	if listArgs["path"] != "docs" {
		t.Fatalf("expected fs.list target mapping, got %#v", listArgs)
	}

	writeArgs := normalizeToolArgs("fs.write", map[string]any{"file": "notes.txt", "text": "hello"})
	if writeArgs["path"] != "notes.txt" || writeArgs["content"] != "hello" {
		t.Fatalf("expected fs.write path/content mapping, got %#v", writeArgs)
	}

	editArgs := normalizeToolArgs("fs.edit", map[string]any{"file": "notes.txt", "find": "a", "replace": "b"})
	if editArgs["path"] != "notes.txt" || editArgs["old"] != "a" || editArgs["new"] != "b" {
		t.Fatalf("expected fs.edit old/new mapping, got %#v", editArgs)
	}

	searchArgs := normalizeToolArgs("code.search", map[string]any{"query": "TODO"})
	if searchArgs["pattern"] != "TODO" {
		t.Fatalf("expected code.search query->pattern mapping, got %#v", searchArgs)
	}

	shellArgs := normalizeToolArgs("shell.exec", map[string]any{"cmd": "pwd"})
	if shellArgs["command"] != "pwd" {
		t.Fatalf("expected shell cmd alias mapping, got %#v", shellArgs)
	}

	wrapped := normalizeToolArgs("shell.exec", map[string]any{"command": "ls -la"})
	if wrapped["command"] != "ls -la" {
		t.Fatalf("expected shell command to be preserved as-is, got %#v", wrapped)
	}
}

func TestShellExecModelOutputCombinesStreams(t *testing.T) {
	output := shellExecModelOutput(map[string]any{
		"stdout": "line 1",
		"stderr": "warn 1",
	})
	if output != "line 1\nwarn 1" {
		t.Fatalf("unexpected combined shell output: %q", output)
	}
}

func TestShellExecModelOutputFallsBackToErrorText(t *testing.T) {
	output := shellExecModelOutput(map[string]any{
		"error": "output_capture_bug: command exited 0 with empty stdout/stderr",
	})
	if !strings.Contains(output, "output_capture_bug") {
		t.Fatalf("expected fallback error text in shell output, got %q", output)
	}
}

func TestShellExecModelOutputHandlesSingleStreamAndEmpty(t *testing.T) {
	stdoutOnly := shellExecModelOutput(map[string]any{"stdout": "ok"})
	if stdoutOnly != "ok" {
		t.Fatalf("expected stdout passthrough, got %q", stdoutOnly)
	}

	stderrOnly := shellExecModelOutput(map[string]any{"stderr": "warn"})
	if stderrOnly != "warn" {
		t.Fatalf("expected stderr passthrough, got %q", stderrOnly)
	}

	empty := shellExecModelOutput(map[string]any{})
	if empty != "" {
		t.Fatalf("expected empty output, got %q", empty)
	}
}

func TestAppendRunConversationPersistsToolAndAssistantMessages(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init engine: %v", err)
	}

	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	out := agent.RunOutput{
		FinalText: "done",
		ToolCalls: []agent.ToolCallRecord{{
			Request: agent.ToolCallRequest{ID: "tool-1", Name: "fs.list"},
			Result:  agent.ToolCallResult{ID: "tool-1", Output: `{"path":".","entries":["a.txt"]}`},
		}},
	}
	if err := e.appendRunConversation(session.SessionID, "run-1", out, true); err != nil {
		t.Fatalf("append run conversation: %v", err)
	}

	msgs, err := store.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected tool + assistant messages, got %d", len(msgs))
	}
	if msgs[0].Role != "tool" || msgs[1].Role != "assistant" {
		t.Fatalf("unexpected appended roles: %+v", msgs)
	}

	if err := e.appendRunConversation(session.SessionID, "run-2", agent.RunOutput{FinalText: "ok"}, false); err != nil {
		t.Fatalf("append assistant-only conversation: %v", err)
	}
}

func TestResolveRunTimeout(t *testing.T) {
	if got := resolveRunTimeout(config.EngineConfig{}); got != defaultRunTimeout {
		t.Fatalf("expected default run timeout %v, got %v", defaultRunTimeout, got)
	}
	if got := resolveRunTimeout(config.EngineConfig{DefaultRunTimeoutMS: 5000, MaxRunTimeoutMS: 3000}); got != 3*time.Second {
		t.Fatalf("expected clamped timeout 3s, got %v", got)
	}
	if got := resolveRunTimeout(config.EngineConfig{DefaultRunTimeoutMS: 0, MaxRunTimeoutMS: 1500}); got != 1500*time.Millisecond {
		t.Fatalf("expected max-based timeout 1500ms, got %v", got)
	}
}

func TestExecuteWithInputAppendsFailureMessageToSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")

	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init engine: %v", err)
	}

	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.ExecuteWithInput(canceledCtx, ExecuteInput{AgentID: "default", Message: "check perplexity secret", Source: "dashboard", SessionID: session.SessionID})
	if err == nil {
		t.Fatal("expected run error from canceled context")
	}

	msgs, err := store.ReadRecentMessages(session.SessionID, 20)
	if err != nil {
		t.Fatalf("read messages: %v", err)
	}

	foundAttention := false
	for _, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		if strings.Contains(strings.ToLower(msg.Content), "need your attention") {
			foundAttention = true
			break
		}
	}
	if !foundAttention {
		t.Fatalf("expected assistant failure attention message, got messages: %+v", msgs)
	}
}

func TestFormatFinalOutputWithThinking(t *testing.T) {
	if got := formatFinalOutputWithThinking("answer", "thought"); got != "answer\n\nThinking:\nthought" {
		t.Fatalf("unexpected formatted output with text: %q", got)
	}
	if got := formatFinalOutputWithThinking("", "thought"); got != "Thinking:\nthought" {
		t.Fatalf("unexpected formatted output without final text: %q", got)
	}
}

func TestRuntimeContextDocIncludesRunTools(t *testing.T) {
	doc := runtimeContextDoc("/tmp/workspace")
	if !strings.Contains(doc, "run.list/run.get") {
		t.Fatalf("expected runtime context to include run tools, got: %s", doc)
	}
	if !strings.Contains(doc, "Run tools") {
		t.Fatalf("expected runtime context to mention Run tools, got: %s", doc)
	}
}

func TestToolCallingBestPracticesDocIncludesRunTools(t *testing.T) {
	doc := toolCallingBestPracticesDoc()
	if !strings.Contains(doc, "run.list") || !strings.Contains(doc, "run.get") {
		t.Fatalf("expected best practices to include run.list and run.get, got: %s", doc)
	}
	if !strings.Contains(doc, "run.list") && !strings.Contains(doc, "run.get") {
		t.Fatalf("expected best practices to mention run tools, got: %s", doc)
	}
}

func TestResolveAgentModelConfigAppliesTimeoutOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Model.TimeoutMS = 90000
	cfg.Agents.AllowAgentModelOverrides = true
	cfg.Agents.Profiles = map[string]config.AgentProfile{
		"alpha": {Model: config.ModelConfig{TimeoutMS: 180000}},
		"beta":  {Model: config.ModelConfig{MaxTokens: 4096}},
	}

	alpha := resolveAgentModelConfig(cfg, "alpha")
	if alpha.TimeoutMS != 180000 {
		t.Fatalf("expected alpha timeout override 180000, got %d", alpha.TimeoutMS)
	}

	beta := resolveAgentModelConfig(cfg, "beta")
	if beta.TimeoutMS != 90000 {
		t.Fatalf("expected beta to inherit global timeout 90000, got %d", beta.TimeoutMS)
	}
}

func TestAllowedToolsIncludesRunTools(t *testing.T) {
	e := &Engine{}
	cfg := config.Default()
	cfg.Network.Enabled = false
	tools := e.allowedTools(cfg)

	hasRunList := false
	hasRunGet := false
	for _, name := range tools {
		if name == "run.list" {
			hasRunList = true
		}
		if name == "run.get" {
			hasRunGet = true
		}
	}
	if !hasRunList {
		t.Fatalf("expected run.list in allowed tools, got: %v", tools)
	}
	if !hasRunGet {
		t.Fatalf("expected run.get in allowed tools, got: %v", tools)
	}
}

// mockAgentRunner captures AgentRunInput for verification.
type mockAgentRunner struct {
	inputs []tools.AgentRunInput
	output tools.AgentRunOutput
	err    error
}

func (m *mockAgentRunner) ExecuteSubAgent(_ context.Context, input tools.AgentRunInput) (tools.AgentRunOutput, error) {
	m.inputs = append(m.inputs, input)
	return m.output, m.err
}

func TestSubAgentAdapterPassesDefaultRestrictions(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-1", FinalText: "ok"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-a",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read", "fs.list"},
			MaxToolIterations: 15,
			TimeoutMS:         5000,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{},
	}

	out, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:  "task-1",
		AgentID: "research",
		Message: "analyze codebase",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success, got %+v", out)
	}
	if len(mock.inputs) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.inputs))
	}
	input := mock.inputs[0]
	if input.InstanceID != "instance-a" {
		t.Fatalf("expected InstanceID=instance-a, got %q", input.InstanceID)
	}
	if input.MessageID != "delegated_task-1_research" {
		t.Fatalf("expected delegated message id, got %q", input.MessageID)
	}
	if len(input.AllowedTools) != 2 || input.AllowedTools[0] != "fs.read" || input.AllowedTools[1] != "fs.list" {
		t.Fatalf("expected default AllowedTools [fs.read, fs.list], got %v", input.AllowedTools)
	}
	if input.MaxToolIterations != 15 {
		t.Fatalf("expected MaxToolIterations=15, got %d", input.MaxToolIterations)
	}
	if input.ThinkingMode != "never" {
		t.Fatalf("expected ThinkingMode=never, got %q", input.ThinkingMode)
	}
}

func TestSubAgentAdapterAppliesOverridesForTargetAgent(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-2", FinalText: "done"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-b",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read", "fs.list"},
			MaxToolIterations: 15,
			TimeoutMS:         5000,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{
			"research": {
				AllowedTools:      []string{"fs.read", "fs.list", "code.search", "http.request"},
				MaxToolIterations: 50,
				ThinkingMode:      "always",
			},
		},
	}

	out, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:  "task-2",
		AgentID: "research",
		Message: "deep dive analysis",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success, got %+v", out)
	}
	if len(mock.inputs) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.inputs))
	}
	input := mock.inputs[0]
	// Override AllowedTools should be used
	if len(input.AllowedTools) != 4 {
		t.Fatalf("expected 4 override AllowedTools, got %v", input.AllowedTools)
	}
	if input.AllowedTools[3] != "http.request" {
		t.Fatalf("expected http.request in AllowedTools, got %v", input.AllowedTools)
	}
	// Override MaxToolIterations should be used
	if input.MaxToolIterations != 50 {
		t.Fatalf("expected MaxToolIterations=50 from override, got %d", input.MaxToolIterations)
	}
	// Override ThinkingMode should be used
	if input.ThinkingMode != "always" {
		t.Fatalf("expected ThinkingMode=always from override, got %q", input.ThinkingMode)
	}
}

func TestSubAgentAdapterFallsBackToDefaultsForUnknownAgent(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-3", FinalText: "ok"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-c",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read"},
			MaxToolIterations: 10,
			TimeoutMS:         3000,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{
			"research": {
				AllowedTools:      []string{"fs.read", "http.request"},
				MaxToolIterations: 50,
			},
		},
	}

	// Agent "builder" has no override — should use defaults.
	_, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:  "task-3",
		AgentID: "builder",
		Message: "build project",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input := mock.inputs[0]
	if len(input.AllowedTools) != 1 || input.AllowedTools[0] != "fs.read" {
		t.Fatalf("expected default AllowedTools [fs.read] for unknown agent, got %v", input.AllowedTools)
	}
	if input.MaxToolIterations != 10 {
		t.Fatalf("expected default MaxToolIterations=10, got %d", input.MaxToolIterations)
	}
}

func TestSubAgentAdapterOverrideMergesWithDefaults(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-4", FinalText: "merged"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-d",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read", "fs.list"},
			MaxToolIterations: 15,
			TimeoutMS:         5000,
			ThinkingMode:      "never",
			DelegationMode:    "prompt_only",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{
			// Only override MaxToolIterations — other fields should fall back to defaults.
			"worker": {
				MaxToolIterations: 100,
			},
		},
	}

	_, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:  "task-4",
		AgentID: "worker",
		Message: "process data",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input := mock.inputs[0]
	// AllowedTools should fall back to defaults since override has none.
	if len(input.AllowedTools) != 2 || input.AllowedTools[0] != "fs.read" {
		t.Fatalf("expected default AllowedTools when override omits them, got %v", input.AllowedTools)
	}
	// MaxToolIterations should use override value.
	if input.MaxToolIterations != 100 {
		t.Fatalf("expected overridden MaxToolIterations=100, got %d", input.MaxToolIterations)
	}
	// ThinkingMode should fall back to default.
	if input.ThinkingMode != "never" {
		t.Fatalf("expected default ThinkingMode=never, got %q", input.ThinkingMode)
	}
}

func TestSubAgentAdapterTaskThinkingModeOverridesConfig(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-5", FinalText: "ok"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-e",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read"},
			MaxToolIterations: 10,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{},
	}

	// Task explicitly sets ThinkingMode — should override config default.
	_, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:       "task-5",
		AgentID:      "analyzer",
		Message:      "think deeply",
		ThinkingMode: "always",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	input := mock.inputs[0]
	if input.ThinkingMode != "always" {
		t.Fatalf("expected task ThinkingMode=always to override config default, got %q", input.ThinkingMode)
	}
}

func TestSubAgentAdapterReturnsMessageIDInOutput(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-msg", MessageID: "delegated_task-msg_default", FinalText: "ok"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-h",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read"},
			MaxToolIterations: 10,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{},
	}

	out, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{TaskID: "task-msg", AgentID: "default", Message: "inspect"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.MessageID != "delegated_task-msg_default" {
		t.Fatalf("expected message id in output, got %+v", out)
	}
}

func TestSubAgentAdapterMarksContinuationHintAsUnsuccessful(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{
		RunID:     "run-interrupted",
		FinalText: "The response stream was interrupted before I could finish. Send `continue` and I will resume from the cutoff point.\n\nLast error: context deadline exceeded",
	}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-f",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read"},
			MaxToolIterations: 10,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{},
	}

	out, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:  "task-interrupted",
		AgentID: "default",
		Message: "finish the delegated task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Success {
		t.Fatalf("expected interruption hint to be marked unsuccessful, got %+v", out)
	}
	if !strings.Contains(out.Error, "interrupted") {
		t.Fatalf("expected interruption error, got %q", out.Error)
	}
}

func TestSubAgentAdapterAppliesRoleConstraints(t *testing.T) {
	mock := &mockAgentRunner{output: tools.AgentRunOutput{RunID: "run-role", FinalText: "ok"}}
	adapter := &agentSubAgentRunnerAdapter{
		runner:     mock,
		instanceID: "instance-g",
		subAgentDefaults: config.SubAgentRestrictions{
			AllowedTools:      []string{"fs.read", "test.run", "check.run", "fs.write"},
			MaxToolIterations: 50,
			TimeoutMS:         9000,
			ThinkingMode:      "never",
		},
		subAgentOverrides: map[string]config.SubAgentRestrictions{},
	}

	_, err := adapter.ExecuteSubAgent(context.Background(), agent.DecomposedTask{
		TaskID:                "task-role",
		AgentID:               "default",
		Message:               "test and verify changes",
		AssignedRole:          "verifier",
		RoleAllowedTools:      []string{"test.run", "check.run"},
		RoleMaxToolIterations: 12,
		RoleTimeoutMS:         2500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.inputs) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.inputs))
	}
	input := mock.inputs[0]

	if input.MaxToolIterations != 12 {
		t.Fatalf("expected role max iterations 12, got %d", input.MaxToolIterations)
	}
	if input.TimeoutMS != 2500 {
		t.Fatalf("expected role timeout 2500, got %d", input.TimeoutMS)
	}
	if len(input.AllowedTools) != 2 || input.AllowedTools[0] != "test.run" || input.AllowedTools[1] != "check.run" {
		t.Fatalf("expected role allowed tools [test.run check.run], got %v", input.AllowedTools)
	}
}
