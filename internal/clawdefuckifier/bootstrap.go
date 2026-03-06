package clawdefuckifier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/agentdocs"
	"openclawssy/internal/config"
	"openclawssy/internal/skillcatalog"
)

type RunCheckpointInput struct {
	AgentID         string
	RunID           string
	TaskID          string
	Source          string
	Status          string
	Message         string
	FinalText       string
	Error           string
	ArtifactPath    string
	ModelProvider   string
	ModelName       string
	StartedAt       time.Time
	CompletedAt     time.Time
	DurationMS      int64
	ToolCalls       int
	ParseRejections int
}

func EnsureBootstrap(agentID, workspaceDir, cfgPath string) error {
	if err := ensureSkill(workspaceDir); err != nil {
		return err
	}
	if !agentdocs.IsClawDefuckifierAgent(agentID) {
		return nil
	}
	if err := ensureConfig(agentID, cfgPath); err != nil {
		return err
	}
	return nil
}

func WriteRunCheckpoint(workspaceDir string, input RunCheckpointInput) (string, error) {
	if !agentdocs.IsClawDefuckifierAgent(input.AgentID) {
		return "", nil
	}
	if strings.TrimSpace(workspaceDir) == "" || strings.TrimSpace(input.RunID) == "" {
		return "", nil
	}
	baseDir := filepath.Join(workspaceDir, "clawdefuckifier", input.AgentID)
	runsDir := filepath.Join(baseDir, "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return "", err
	}
	body := checkpointMarkdown(input)
	path := filepath.Join(runsDir, input.RunID+".md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(baseDir, "LATEST.md"), []byte(body), 0o600); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(workspaceDir, path)
	if err != nil {
		return filepath.ToSlash(path), nil
	}
	return filepath.ToSlash(rel), nil
}

func ensureSkill(workspaceDir string) error {
	if strings.TrimSpace(workspaceDir) == "" {
		return nil
	}
	body, ok := skillcatalog.Body("clawdefuckifier")
	if !ok {
		return nil
	}
	skillsDir := filepath.Join(workspaceDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(skillsDir, "clawdefuckifier.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

func ensureConfig(agentID, cfgPath string) error {
	if strings.TrimSpace(cfgPath) == "" {
		return nil
	}
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		return err
	}
	changed := false
	if !cfg.Agents.AllowInterAgentMessaging {
		cfg.Agents.AllowInterAgentMessaging = true
		changed = true
	}
	if !cfg.Agents.SelfImprovementEnabled {
		cfg.Agents.SelfImprovementEnabled = true
		changed = true
	}
	profile := cfg.Agents.Profiles[agentID]
	if profile.Enabled == nil || !*profile.Enabled {
		enabled := true
		profile.Enabled = &enabled
		changed = true
	}
	if !profile.SelfImprovement {
		profile.SelfImprovement = true
		changed = true
	}
	cfg.Agents.Profiles[agentID] = profile
	if !changed {
		return nil
	}
	return config.Save(cfgPath, cfg)
}

func checkpointMarkdown(input RunCheckpointInput) string {
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "unknown"
	}
	var b strings.Builder
	b.WriteString("# ClawDefuckifier Checkpoint\n\n")
	writeCheckpointField(&b, "agent_id", input.AgentID)
	writeCheckpointField(&b, "run_id", input.RunID)
	writeCheckpointField(&b, "task_id", input.TaskID)
	writeCheckpointField(&b, "status", status)
	writeCheckpointField(&b, "source", input.Source)
	writeCheckpointField(&b, "model", joinModel(input.ModelProvider, input.ModelName))
	writeCheckpointField(&b, "started_at", formatCheckpointTime(input.StartedAt))
	writeCheckpointField(&b, "completed_at", formatCheckpointTime(input.CompletedAt))
	writeCheckpointField(&b, "duration_ms", formatInt(input.DurationMS))
	writeCheckpointField(&b, "tool_calls", formatInt(int64(input.ToolCalls)))
	writeCheckpointField(&b, "parse_rejections", formatInt(int64(input.ParseRejections)))
	writeCheckpointField(&b, "artifact_path", input.ArtifactPath)
	b.WriteString("\n## Message\n")
	b.WriteString(trimCheckpointBlock(input.Message, 2000))
	b.WriteString("\n\n## Outcome\n")
	if strings.TrimSpace(input.Error) != "" {
		b.WriteString("ERROR: ")
		b.WriteString(trimCheckpointBlock(input.Error, 2000))
	} else {
		b.WriteString(trimCheckpointBlock(input.FinalText, 4000))
	}
	b.WriteString("\n")
	return b.String()
}

func writeCheckpointField(b *strings.Builder, key, value string) {
	b.WriteString("- ")
	b.WriteString(strings.TrimSpace(key))
	b.WriteString(": ")
	if strings.TrimSpace(value) == "" {
		b.WriteString("-\n")
		return
	}
	b.WriteString(strings.TrimSpace(value))
	b.WriteString("\n")
}

func trimCheckpointBlock(value string, maxChars int) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return "-"
	}
	if maxChars > 0 && len(text) > maxChars {
		return text[:maxChars] + "\n\n[truncated]"
	}
	return text
}

func formatCheckpointTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func joinModel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return model
	}
	if model == "" {
		return provider
	}
	return provider + "/" + model
}

func formatInt(value int64) string {
	if value == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", value)
}
