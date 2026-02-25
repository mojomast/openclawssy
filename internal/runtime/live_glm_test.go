//go:build liveglm

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
)

func TestLiveGLMToolCallsCompleteWithoutLooping(t *testing.T) {
	if strings.TrimSpace(os.Getenv("OPENCLAWSSY_LIVE_GLM")) != "1" {
		t.Skip("set OPENCLAWSSY_LIVE_GLM=1 to run live GLM-4.7 integration checks")
	}
	apiKey := strings.TrimSpace(os.Getenv("ZAI_API_KEY"))
	if apiKey == "" {
		t.Skip("set ZAI_API_KEY to run live GLM-4.7 integration checks")
	}

	iterations := 2
	if raw := strings.TrimSpace(os.Getenv("OPENCLAWSSY_LIVE_GLM_ITERATIONS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid OPENCLAWSSY_LIVE_GLM_ITERATIONS=%q", raw)
		}
		iterations = parsed
	}
	scenarioFilter := strings.TrimSpace(os.Getenv("OPENCLAWSSY_LIVE_GLM_SCENARIO"))

	root := t.TempDir()
	engine, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Init("default", false); err != nil {
		t.Fatalf("engine init: %v", err)
	}
	workspaceReadme := filepath.Join(root, "workspace", "README.md")
	if err := os.WriteFile(workspaceReadme, []byte("# Live GLM Workspace\n\nfixture\n"), 0o600); err != nil {
		t.Fatalf("write workspace README fixture: %v", err)
	}
	workspaceStatus := filepath.Join(root, "workspace", "PROJECT_STATUS.md")
	statusFixture := "# Project Status\n\nOpenclawssy is a prototype in development.\nDo not deploy this to production.\nMaturity label: Prototype / Builder Preview.\n"
	if err := os.WriteFile(workspaceStatus, []byte(statusFixture), 0o600); err != nil {
		t.Fatalf("write workspace PROJECT_STATUS fixture: %v", err)
	}
	workspaceArch := filepath.Join(root, "workspace", "ARCHITECTURE.md")
	archFixture := "# Architecture\n\nThe runtime uses a model/tool loop with bounded iteration and loop-prevention guards.\nRuns persist audit traces and artifacts for reproducibility.\n"
	if err := os.WriteFile(workspaceArch, []byte(archFixture), 0o600); err != nil {
		t.Fatalf("write workspace ARCHITECTURE fixture: %v", err)
	}
	workspaceDevplan := filepath.Join(root, "workspace", "DEVPLAN_SIMPLE.md")
	devplanFixture := "# DEVPLAN_SIMPLE\n\nGoal: implement a tiny coding task from this plan.\n\nTasks:\n1. Create calculator.py with function add(a, b) that returns a + b.\n2. Create handoff.txt containing exactly: status: implemented\n"
	if err := os.WriteFile(workspaceDevplan, []byte(devplanFixture), 0o600); err != nil {
		t.Fatalf("write workspace DEVPLAN fixture: %v", err)
	}
	probeToken := "orchid cactus lantern"
	sourceProbePath := filepath.Join(root, "workspace", "source_probe.txt")
	if err := os.WriteFile(sourceProbePath, []byte(probeToken+"\n"), 0o600); err != nil {
		t.Fatalf("write source probe fixture: %v", err)
	}

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "zai"
	cfg.Model.Name = "GLM-4.7"
	cfg.Providers.ZAI.APIKeyEnv = ""
	cfg.Providers.ZAI.APIKey = apiKey
	cfg.Sandbox.Active = false
	cfg.Sandbox.Provider = "none"
	cfg.Shell.EnableExec = false
	cfg.Network.Enabled = false
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	type scenario struct {
		name      string
		message   string
		minTools  int
		maxTools  int
		validator func(*testing.T, RunResult)
	}

	scenarios := []scenario{
		{
			name:     "readme-heading",
			message:  "Read README.md and reply with the exact text from line 1 only.",
			minTools: 1,
			maxTools: 6,
			validator: func(t *testing.T, res RunResult) {
				if !strings.Contains(strings.ToLower(res.FinalText), "live glm workspace") {
					t.Fatalf("expected README heading reference, got %q", res.FinalText)
				}
			},
		},
		{
			name:     "write-then-readback",
			message:  "Read source_probe.txt, copy its exact text into live_glm_probe.txt, then reply with only the copied text.",
			minTools: 1,
			maxTools: 8,
			validator: func(t *testing.T, res RunResult) {
				copiedPath := filepath.Join(root, "workspace", "live_glm_probe.txt")
				copied, err := os.ReadFile(copiedPath)
				if err != nil {
					t.Fatalf("expected copied file to exist: %v", err)
				}
				if strings.TrimSpace(string(copied)) != probeToken {
					t.Fatalf("expected copied file content %q, got %q", probeToken, strings.TrimSpace(string(copied)))
				}
				if strings.TrimSpace(res.FinalText) == "" {
					t.Fatalf("expected non-empty assistant confirmation after copy, got %q", res.FinalText)
				}
			},
		},
		{
			name:     "research-summary",
			message:  "Research PROJECT_STATUS.md and ARCHITECTURE.md, then return exactly 3 concise bullets: state, risk, and architecture note. Include the exact phrase 'Do not deploy this to production.' if present.",
			minTools: 1,
			maxTools: 10,
			validator: func(t *testing.T, res RunResult) {
				lower := strings.ToLower(res.FinalText)
				if !strings.Contains(lower, "prototype") {
					t.Fatalf("expected prototype finding in research summary, got %q", res.FinalText)
				}
				if !strings.Contains(lower, "do not deploy this to production") && !strings.Contains(lower, "risk") {
					t.Fatalf("expected production/risk finding in research summary, got %q", res.FinalText)
				}
				if !strings.Contains(lower, "loop") && !strings.Contains(lower, "bounded") && !strings.Contains(lower, "tool") && !strings.Contains(lower, "policy") && !strings.Contains(lower, "audit") && !strings.Contains(lower, "artifact") {
					t.Fatalf("expected architecture loop/guard finding in research summary, got %q", res.FinalText)
				}
			},
		},
		{
			name:     "coding-from-devplan",
			message:  "Read DEVPLAN_SIMPLE.md and implement it exactly by creating/updating all requested files in the workspace. Use tool calls only as fenced JSON objects with tool_name and arguments. Do not use <tool_call> tags. First read DEVPLAN_SIMPLE.md, then create the requested files, then give a short completion confirmation.",
			minTools: 1,
			maxTools: 24,
			validator: func(t *testing.T, res RunResult) {
				calculatorPath := filepath.Join(root, "workspace", "calculator.py")
				calculator, err := os.ReadFile(calculatorPath)
				if err != nil {
					t.Fatalf("expected calculator.py to exist: %v", err)
				}
				calcText := strings.ToLower(string(calculator))
				if !strings.Contains(calcText, "def add") || !strings.Contains(calcText, "return") {
					t.Fatalf("expected add implementation in calculator.py, got %q", string(calculator))
				}

				handoffPath := filepath.Join(root, "workspace", "handoff.txt")
				handoff, err := os.ReadFile(handoffPath)
				if err != nil {
					t.Fatalf("expected handoff.txt to exist: %v", err)
				}
				if strings.TrimSpace(strings.ToLower(string(handoff))) != "status: implemented" {
					t.Fatalf("expected handoff.txt marker, got %q", strings.TrimSpace(string(handoff)))
				}

				if strings.TrimSpace(res.FinalText) == "" {
					t.Fatalf("expected non-empty completion confirmation, got %q", res.FinalText)
				}
			},
		},
	}

	for i := 1; i <= iterations; i++ {
		for _, sc := range scenarios {
			if scenarioFilter != "" && sc.name != scenarioFilter {
				continue
			}
			t.Run("iter-"+strconv.Itoa(i)+"/"+sc.name, func(t *testing.T) {
				if sc.name == "coding-from-devplan" {
					for _, path := range []string{
						filepath.Join(root, "workspace", "calculator.py"),
						filepath.Join(root, "workspace", "test_calculator.py"),
						filepath.Join(root, "workspace", "handoff.txt"),
					} {
						if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("cleanup %s: %v", path, err)
						}
					}
				}

				session, err := engine.chatStore.CreateSession(chatstore.CreateSessionInput{
					AgentID: "default",
					Channel: "dashboard",
					UserID:  "liveglm",
					RoomID:  "liveglm-room",
					Title:   "Live GLM tool-loop regression",
				})
				if err != nil {
					t.Fatalf("create session: %v", err)
				}

				if sc.name == "coding-from-devplan" {
					const maxAttempts = 3
					totalToolCalls := 0
					var lastRes RunResult
					var lastValidationErr error
					for attempt := 1; attempt <= maxAttempts; attempt++ {
						message := sc.message
						if attempt > 1 {
							message = "Previous attempt was incomplete. Read DEVPLAN_SIMPLE.md and create BOTH files exactly: calculator.py with add(a, b) returning a + b, and handoff.txt containing exactly 'status: implemented'. Use JSON tool calls only and finish now."
						}

						runCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
						res, err := engine.ExecuteWithInput(runCtx, ExecuteInput{
							AgentID:   "default",
							SessionID: session.SessionID,
							Source:    "dashboard",
							Message:   message,
						})
						cancel()
						if err != nil {
							t.Fatalf("execute with live GLM failed: %v", err)
						}
						lastRes = res
						totalToolCalls += res.ToolCalls

						lower := strings.ToLower(res.FinalText)
						blockedPhrases := []string{
							"need your help to continue",
							"tool-iteration limit",
							"repetition detected",
							"could not complete an actionable execution step",
							"/resume",
						}
						for _, phrase := range blockedPhrases {
							if strings.Contains(lower, phrase) {
								t.Fatalf("unexpected loop/escalation phrase %q in final text: %q", phrase, res.FinalText)
							}
						}

						lastValidationErr = validateCodingDevplanArtifacts(root, res)
						if lastValidationErr == nil {
							break
						}
					}
					if lastValidationErr != nil {
						t.Fatalf("coding-from-devplan did not complete after retries: %v (last final=%q)", lastValidationErr, lastRes.FinalText)
					}
					if totalToolCalls < sc.minTools {
						t.Fatalf("expected at least %d cumulative tool call(s), got %d", sc.minTools, totalToolCalls)
					}
					if totalToolCalls > (sc.maxTools * maxAttempts) {
						t.Fatalf("expected <= %d cumulative tool calls, got %d", sc.maxTools*maxAttempts, totalToolCalls)
					}
					return
				}

				runCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
				defer cancel()

				res, err := engine.ExecuteWithInput(runCtx, ExecuteInput{
					AgentID:   "default",
					SessionID: session.SessionID,
					Source:    "dashboard",
					Message:   sc.message,
				})
				if err != nil {
					t.Fatalf("execute with live GLM failed: %v", err)
				}

				if strings.TrimSpace(res.FinalText) == "" {
					t.Fatal("expected non-empty final response")
				}
				if res.ToolCalls < sc.minTools {
					t.Fatalf("expected at least %d tool call(s), got %d (final=%q)", sc.minTools, res.ToolCalls, res.FinalText)
				}
				if res.ToolCalls > sc.maxTools {
					t.Fatalf("expected <= %d tool calls to avoid loops, got %d", sc.maxTools, res.ToolCalls)
				}

				lower := strings.ToLower(res.FinalText)
				blockedPhrases := []string{
					"need your help to continue",
					"tool-iteration limit",
					"repetition detected",
					"could not complete an actionable execution step",
					"/resume",
				}
				for _, phrase := range blockedPhrases {
					if strings.Contains(lower, phrase) {
						t.Fatalf("unexpected loop/escalation phrase %q in final text: %q", phrase, res.FinalText)
					}
				}

				sc.validator(t, res)
			})
		}
	}
}

func validateCodingDevplanArtifacts(root string, res RunResult) error {
	calculatorPath := filepath.Join(root, "workspace", "calculator.py")
	calculator, err := os.ReadFile(calculatorPath)
	if err != nil {
		return fmt.Errorf("expected calculator.py to exist: %w", err)
	}
	calcText := strings.ToLower(string(calculator))
	if !strings.Contains(calcText, "def add") || !strings.Contains(calcText, "return") {
		return fmt.Errorf("expected add implementation in calculator.py, got %q", string(calculator))
	}

	handoffPath := filepath.Join(root, "workspace", "handoff.txt")
	handoff, err := os.ReadFile(handoffPath)
	if err != nil {
		return fmt.Errorf("expected handoff.txt to exist: %w", err)
	}
	if strings.TrimSpace(strings.ToLower(string(handoff))) != "status: implemented" {
		return fmt.Errorf("expected handoff.txt marker, got %q", strings.TrimSpace(string(handoff)))
	}

	if strings.TrimSpace(res.FinalText) == "" {
		return fmt.Errorf("expected non-empty completion confirmation, got %q", res.FinalText)
	}
	return nil
}
