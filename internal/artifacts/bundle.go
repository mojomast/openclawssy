package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"openclawssy/internal/fsutil"
)

const baseBundleDir = ".openclawssy"

// WriteRunBundle writes a run bundle under:
// .openclawssy/agents/<agentID>/runs/<runID>/
func WriteRunBundle(rootDir, agentID, runID string, input, output, toolCalls, meta any) (string, error) {
	runDir := filepath.Join(rootDir, baseBundleDir, "agents", agentID, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("create run bundle dir: %w", err)
	}

	if err := writeJSONAtomic(filepath.Join(runDir, "input.json"), input); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "output.json"), output); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "tool_calls.json"), toolCalls); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "meta.json"), meta); err != nil {
		return "", err
	}

	return runDir, nil
}

type BundleV1Input struct {
	Input      any
	PromptMD   string
	ToolCalls  []string
	OutputMD   string
	Meta       map[string]any
	MirrorJSON bool
}

func WriteRunBundleV1(rootDir, agentID, runID string, payload BundleV1Input) (string, error) {
	runDir := filepath.Join(rootDir, baseBundleDir, "agents", agentID, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("create run bundle dir: %w", err)
	}
	if payload.Meta == nil {
		payload.Meta = map[string]any{}
	}
	payload.Meta["bundle_version"] = 1

	if err := writeJSONAtomic(filepath.Join(runDir, "input.json"), payload.Input); err != nil {
		return "", err
	}
	if err := writeTextAtomic(filepath.Join(runDir, "prompt.md"), payload.PromptMD); err != nil {
		return "", err
	}
	if err := writeJSONLAtomic(filepath.Join(runDir, "toolcalls.jsonl"), payload.ToolCalls); err != nil {
		return "", err
	}
	if err := writeTextAtomic(filepath.Join(runDir, "output.md"), payload.OutputMD); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "meta.json"), payload.Meta); err != nil {
		return "", err
	}

	if payload.MirrorJSON {
		_ = writeJSONAtomic(filepath.Join(runDir, "output.json"), map[string]any{"final_text": payload.OutputMD})
		_ = writeJSONAtomic(filepath.Join(runDir, "tool_calls.json"), payload.ToolCalls)
	}

	return runDir, nil
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')

	return fsutil.WriteFileAtomic(path, data, 0o600)
}

func writeTextAtomic(path string, body string) error {
	if body != "" && body[len(body)-1] != '\n' {
		body += "\n"
	}
	return fsutil.WriteFileAtomic(path, []byte(body), 0o600)
}

func writeJSONLAtomic(path string, lines []string) error {
	buf := make([]byte, 0, len(lines)*64)
	for _, line := range lines {
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return fsutil.WriteFileAtomic(path, buf, 0o600)
}
