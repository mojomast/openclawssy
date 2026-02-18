package toolparse

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"openclawssy/internal/agent"
)

var toolAliases = map[string]string{
	"fs.read":          "fs.read",
	"fs.list":          "fs.list",
	"fs.write":         "fs.write",
	"fs.append":        "fs.append",
	"fs.delete":        "fs.delete",
	"fs.move":          "fs.move",
	"fs.rename":        "fs.move",
	"fs.edit":          "fs.edit",
	"code.search":      "code.search",
	"config.get":       "config.get",
	"config.set":       "config.set",
	"secrets.get":      "secrets.get",
	"secrets.set":      "secrets.set",
	"secrets.list":     "secrets.list",
	"skill.list":       "skill.list",
	"skill.read":       "skill.read",
	"skill.get":        "skill.read",
	"scheduler.list":   "scheduler.list",
	"scheduler.add":    "scheduler.add",
	"scheduler.remove": "scheduler.remove",
	"scheduler.pause":  "scheduler.pause",
	"scheduler.resume": "scheduler.resume",
	"session.list":     "session.list",
	"session.close":    "session.close",
	"agent.list":       "agent.list",
	"agent.create":     "agent.create",
	"agent.switch":     "agent.switch",
	"policy.list":      "policy.list",
	"policy.grant":     "policy.grant",
	"policy.revoke":    "policy.revoke",
	"run.list":         "run.list",
	"run.get":          "run.get",
	"run.cancel":       "run.cancel",
	"metrics.get":      "metrics.get",
	"http.request":     "http.request",
	"net.fetch":        "http.request",
	"time.now":         "time.now",
	"shell.exec":       "shell.exec",
	"bash.exec":        "shell.exec",
	"terminal.exec":    "shell.exec",
	"terminal.run":     "shell.exec",
}

type Extraction struct {
	RawSnippet      string
	ParsedToolName  string
	ParsedArguments json.RawMessage
	Accepted        bool
	Reason          string
}

type ParseResult struct {
	Calls       []agent.ToolCallRequest
	Extractions []Extraction
}

type ParseDiagnostics struct {
	Candidates []Extraction
	Rejected   []Extraction
}

const defaultMaxToolCallsPerReply = 6

func ParseToolCalls(text string, allowedTools []string) ([]agent.ToolCallRequest, ParseDiagnostics) {
	return parseToolCalls(text, allowedTools, defaultMaxToolCallsPerReply)
}

func ParseStrict(content string, allowedTools []string, maxCalls int) ParseResult {
	if maxCalls <= 0 {
		maxCalls = 1
	}
	calls, diag := parseToolCalls(content, allowedTools, maxCalls)
	return ParseResult{Calls: calls, Extractions: diag.Candidates}
}

func CanonicalToolName(name string) (string, bool) {
	candidate := strings.ToLower(strings.TrimSpace(name))
	if candidate == "" {
		return "", false
	}
	toolName, ok := toolAliases[candidate]
	return toolName, ok
}

func IsAllowed(toolName string, allowed []string) bool {
	if strings.TrimSpace(toolName) == "" || toolName == "tool.result" {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, raw := range allowed {
		candidate, ok := CanonicalToolName(raw)
		if !ok {
			candidate = strings.ToLower(strings.TrimSpace(raw))
		}
		if candidate == toolName {
			return true
		}
	}
	return false
}

type parseCandidate struct {
	RawSnippet string
	JSONText   string
}

func parseToolCalls(content string, allowedTools []string, maxCalls int) ([]agent.ToolCallRequest, ParseDiagnostics) {
	if maxCalls <= 0 {
		maxCalls = 1
	}

	diag := ParseDiagnostics{}
	candidates := collectCandidates(content)
	if len(candidates) == 0 {
		return nil, diag
	}

	calls := make([]agent.ToolCallRequest, 0, min(maxCalls, len(candidates)))
	nextID := 1
	for _, candidate := range candidates {
		entries, parsedCalls := parseCandidateBlock(candidate, allowedTools, &nextID)
		diag.Candidates = append(diag.Candidates, entries...)
		for _, entry := range entries {
			if !entry.Accepted {
				diag.Rejected = append(diag.Rejected, entry)
			}
		}
		for _, call := range parsedCalls {
			if len(calls) < maxCalls {
				calls = append(calls, call)
			}
		}
	}

	return calls, diag
}

func collectCandidates(content string) []parseCandidate {
	re := regexp.MustCompile("(?is)```(?:json|tool)\\s*([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(content, -1)
	candidates := make([]parseCandidate, 0, len(matches)+2)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		candidates = append(candidates, parseCandidate{
			RawSnippet: strings.TrimSpace(match[0]),
			JSONText:   strings.TrimSpace(match[1]),
		})
	}

	withoutFences := re.ReplaceAllString(content, " ")
	for _, raw := range extractBalancedJSONCandidates(withoutFences) {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		candidates = append(candidates, parseCandidate{RawSnippet: candidate, JSONText: candidate})
	}

	return candidates
}

func parseCandidateBlock(candidate parseCandidate, allowedTools []string, nextID *int) ([]Extraction, []agent.ToolCallRequest) {
	rawSnippet := strings.TrimSpace(candidate.RawSnippet)
	jsonText := strings.TrimSpace(candidate.JSONText)
	if rawSnippet == "" {
		rawSnippet = jsonText
	}
	if jsonText == "" {
		entry := Extraction{RawSnippet: rawSnippet, Reason: "empty candidate block"}
		return []Extraction{entry}, nil
	}

	decoded, parseErr := decodeJSONCandidate(jsonText)
	if parseErr != "" {
		entry := Extraction{RawSnippet: rawSnippet, Reason: parseErr}
		return []Extraction{entry}, nil
	}

	switch value := decoded.(type) {
	case map[string]any:
		entry, call, ok := parseToolObject(rawSnippet, value, allowedTools, nextID)
		if !ok {
			return []Extraction{entry}, nil
		}
		return []Extraction{entry}, []agent.ToolCallRequest{call}
	case []any:
		if len(value) == 0 {
			entry := Extraction{RawSnippet: rawSnippet, Reason: "empty tool call array"}
			return []Extraction{entry}, nil
		}
		entries := make([]Extraction, 0, len(value))
		calls := make([]agent.ToolCallRequest, 0, len(value))
		for i, item := range value {
			itemRaw := rawJSON(item)
			obj, ok := item.(map[string]any)
			if !ok {
				entries = append(entries, Extraction{
					RawSnippet: itemRaw,
					Reason:     "array element " + strconv.Itoa(i+1) + " is not a JSON object",
				})
				continue
			}
			entry, call, accepted := parseToolObject(itemRaw, obj, allowedTools, nextID)
			entries = append(entries, entry)
			if accepted {
				calls = append(calls, call)
			}
		}
		return entries, calls
	default:
		entry := Extraction{RawSnippet: rawSnippet, Reason: "candidate must be a JSON object or array"}
		return []Extraction{entry}, nil
	}
}

func decodeJSONCandidate(jsonText string) (any, string) {
	var decoded any
	if err := json.Unmarshal([]byte(jsonText), &decoded); err == nil {
		return decoded, ""
	} else {
		repaired := repairJSONText(jsonText)
		if repaired == "" || repaired == jsonText {
			return nil, fmt.Sprintf("invalid JSON: %v", err)
		}
		if errRepair := json.Unmarshal([]byte(repaired), &decoded); errRepair != nil {
			return nil, fmt.Sprintf("invalid JSON: %v (repair failed: %v)", err, errRepair)
		}
		return decoded, ""
	}
}

func repairJSONText(text string) string {
	repaired := strings.TrimSpace(text)
	if repaired == "" {
		return ""
	}

	repaired = unwrapFence(repaired)

	if extracted, ok := extractSingleTopLevelJSON(repaired); ok {
		repaired = extracted
	}

	repaired = stripTrailingCommas(repaired)
	return strings.TrimSpace(repaired)
}

func unwrapFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return trimmed
	}

	firstNewline := strings.IndexByte(trimmed, '\n')
	if firstNewline < 0 {
		return trimmed
	}
	body := strings.TrimSpace(trimmed[firstNewline+1:])
	if !strings.HasSuffix(body, "```") {
		return trimmed
	}
	body = strings.TrimSuffix(body, "```")
	return strings.TrimSpace(body)
}

func extractSingleTopLevelJSON(text string) (string, bool) {
	candidates := extractBalancedJSONCandidates(text)
	if len(candidates) != 1 {
		return "", false
	}
	return strings.TrimSpace(candidates[0]), true
}

func stripTrailingCommas(text string) string {
	if text == "" {
		return text
	}

	var b strings.Builder
	b.Grow(len(text))

	inString := false
	escaped := false

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			b.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}

		if ch == ',' {
			j := i + 1
			for j < len(text) {
				next := text[j]
				if next == ' ' || next == '\n' || next == '\r' || next == '\t' {
					j++
					continue
				}
				break
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				continue
			}
		}

		b.WriteByte(ch)
	}

	return b.String()
}

func parseToolObject(rawSnippet string, obj map[string]any, allowedTools []string, nextID *int) (Extraction, agent.ToolCallRequest, bool) {
	entry := Extraction{RawSnippet: strings.TrimSpace(rawSnippet)}

	rawName, ok := obj["tool_name"]
	if !ok {
		entry.Reason = "missing tool_name field"
		return entry, agent.ToolCallRequest{}, false
	}
	toolNameInput, ok := rawName.(string)
	if !ok {
		entry.Reason = "invalid tool_name field"
		return entry, agent.ToolCallRequest{}, false
	}
	toolName, ok := CanonicalToolName(toolNameInput)
	if !ok || toolName == "" {
		entry.ParsedToolName = strings.TrimSpace(toolNameInput)
		entry.Reason = "unsupported tool name"
		return entry, agent.ToolCallRequest{}, false
	}
	entry.ParsedToolName = toolName

	if toolName == "tool.result" {
		entry.Reason = "tool.result is output-only"
		return entry, agent.ToolCallRequest{}, false
	}
	if !IsAllowed(toolName, allowedTools) {
		entry.Reason = fmt.Sprintf("tool %q not allowed", toolName)
		return entry, agent.ToolCallRequest{}, false
	}

	argsValue, hasArgs := obj["arguments"]
	if !hasArgs {
		entry.Reason = "missing arguments field"
		return entry, agent.ToolCallRequest{}, false
	}
	typedArgs, ok := argsValue.(map[string]any)
	if !ok {
		entry.Reason = "arguments must be a JSON object"
		return entry, agent.ToolCallRequest{}, false
	}
	argBytes, _ := json.Marshal(typedArgs)

	callID, idErr := parseCallID(obj, nextID)
	if idErr != "" {
		entry.Reason = idErr
		return entry, agent.ToolCallRequest{}, false
	}

	entry.ParsedArguments = argBytes
	entry.Accepted = true

	call := agent.ToolCallRequest{
		ID:        callID,
		Name:      toolName,
		Arguments: argBytes,
	}

	return entry, call, true
}

func parseCallID(obj map[string]any, nextID *int) (string, string) {
	rawID, hasID := obj["id"]
	if hasID {
		id, ok := rawID.(string)
		if !ok || strings.TrimSpace(id) == "" {
			return "", "id must be a non-empty string"
		}
		return strings.TrimSpace(id), ""
	}

	id := fmt.Sprintf("tool-json-%d", *nextID)
	(*nextID)++
	return id, ""
}

func extractBalancedJSONCandidates(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	candidates := make([]string, 0, 4)
	start := -1
	stack := make([]rune, 0, 16)
	inString := false
	escaped := false

	for i, r := range content {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}

		if r == '"' {
			inString = true
			continue
		}

		if r == '{' || r == '[' {
			if len(stack) == 0 {
				start = i
			}
			stack = append(stack, r)
			continue
		}

		if r == '}' || r == ']' {
			if len(stack) == 0 {
				continue
			}

			last := stack[len(stack)-1]
			if (r == '}' && last == '{') || (r == ']' && last == '[') {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 && start >= 0 {
					candidates = append(candidates, strings.TrimSpace(content[start:i+1]))
					start = -1
				}
			} else {
				// Mismatch found (e.g. found '}' but expected ']')
				// Abort current candidate
				stack = stack[:0]
				start = -1
			}
		}
	}

	return candidates
}

func rawJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
