package toolparse

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseStrictIgnoresShellExplanation(t *testing.T) {
	content := "I will check this with ls and then summarize."
	res := ParseStrict(content, []string{"fs.list"}, 1)
	if len(res.Calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.Calls))
	}
}

func TestParseStrictRejectsToolResultObject(t *testing.T) {
	content := "```json\n{\"tool_name\":\"tool.result\",\"arguments\":{}}\n```"
	res := ParseStrict(content, nil, 1)
	if len(res.Calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.Calls))
	}
	if len(res.Extractions) == 0 || res.Extractions[0].Reason == "" {
		t.Fatalf("expected extraction reason, got %#v", res.Extractions)
	}
}

func TestParseStrictParsesValidToolJSONBlock(t *testing.T) {
	content := "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"
	res := ParseStrict(content, []string{"fs.list"}, 1)
	if len(res.Calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(res.Calls))
	}
	if res.Calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", res.Calls[0].Name)
	}
}

func TestParseStrictLimitsToSingleCall(t *testing.T) {
	content := "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```\n```json\n{\"tool_name\":\"fs.read\",\"arguments\":{\"path\":\"a.txt\"}}\n```"
	res := ParseStrict(content, []string{"fs.list", "fs.read"}, 1)
	if len(res.Calls) != 1 {
		t.Fatalf("expected one call due to strict max=1, got %d", len(res.Calls))
	}
	if res.Calls[0].Name != "fs.list" {
		t.Fatalf("expected first call only, got %q", res.Calls[0].Name)
	}
}

func TestParseStrictDisallowedToolReasonIsExplicit(t *testing.T) {
	content := "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```"
	res := ParseStrict(content, []string{"fs.list"}, 1)
	if len(res.Calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.Calls))
	}
	if len(res.Extractions) != 1 {
		t.Fatalf("expected one extraction, got %d", len(res.Extractions))
	}
	reason := res.Extractions[0].Reason
	if !strings.Contains(reason, "shell.exec") || !strings.Contains(reason, "not allowed") {
		t.Fatalf("expected explicit disallowed-tool reason, got %q", reason)
	}
}

func TestParseToolCallsParsesInlineJSONObjectCandidate(t *testing.T) {
	text := `I'll do it now: {"tool_name":"fs.list","arguments":{"path":"."}}`
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", calls[0].Name)
	}
	if len(diag.Candidates) == 0 {
		t.Fatal("expected diagnostics candidates")
	}
}

func TestParseToolCallsParsesFencedToolBlock(t *testing.T) {
	text := "```tool\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", calls[0].Name)
	}
}

func TestParseToolCallsParsesArrayOfToolCalls(t *testing.T) {
	text := `[{"tool_name":"fs.list","arguments":{"path":"."}},{"tool_name":"fs.read","arguments":{"path":"README.md"}}]`
	calls, _ := ParseToolCalls(text, []string{"fs.list", "fs.read"})
	if len(calls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" || calls[1].Name != "fs.read" {
		t.Fatalf("unexpected call names: %+v", calls)
	}
}

func TestParseToolCallsDeterministicGeneratedIDsWithoutIDs(t *testing.T) {
	text := `[{"tool_name":"fs.list","arguments":{"path":"."}},{"tool_name":"fs.read","arguments":{"path":"README.md"}}]`
	first, _ := ParseToolCalls(text, []string{"fs.list", "fs.read"})
	second, _ := ParseToolCalls(text, []string{"fs.list", "fs.read"})

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected two calls from each parse, got %d and %d", len(first), len(second))
	}
	if first[0].ID != "tool-json-1" || first[1].ID != "tool-json-2" {
		t.Fatalf("unexpected generated ids: %q, %q", first[0].ID, first[1].ID)
	}
	if first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatalf("expected deterministic ids across parses, got %+v and %+v", first, second)
	}
}

func TestParseToolCallsCanonicalizesAliases(t *testing.T) {
	text := "```json\n{\"tool_name\":\"bash.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"shell.exec"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "shell.exec" {
		t.Fatalf("expected canonical shell.exec, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsFsDeleteWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"fs.delete\",\"arguments\":{\"path\":\"old.txt\",\"force\":true}}\n```"
	calls, _ := ParseToolCalls(text, []string{"fs.delete"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.delete" {
		t.Fatalf("expected fs.delete, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsFsAppendWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"fs.append\",\"arguments\":{\"path\":\"notes.txt\",\"content\":\"more\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"fs.append"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.append" {
		t.Fatalf("expected fs.append, got %q", calls[0].Name)
	}
}

func TestParseToolCallsCanonicalizesFsRenameAlias(t *testing.T) {
	text := "```json\n{\"tool_name\":\"fs.rename\",\"arguments\":{\"src\":\"a.txt\",\"dst\":\"b.txt\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"fs.move"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.move" {
		t.Fatalf("expected canonical fs.move, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsConfigSetWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"config.set\",\"arguments\":{\"updates\":{\"output.thinking_mode\":\"always\"}}}\n```"
	calls, _ := ParseToolCalls(text, []string{"config.set"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "config.set" {
		t.Fatalf("expected config.set, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsSecretsGetWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"secrets.get\",\"arguments\":{\"key\":\"provider/openrouter/api_key\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"secrets.get"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "secrets.get" {
		t.Fatalf("expected secrets.get, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsSchedulerAddWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"scheduler.add\",\"arguments\":{\"schedule\":\"@every 1h\",\"message\":\"status\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"scheduler.add"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "scheduler.add" {
		t.Fatalf("expected scheduler.add, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsSessionCloseWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"session.close\",\"arguments\":{\"session_id\":\"chat_123\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"session.close"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "session.close" {
		t.Fatalf("expected session.close, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsAgentToolsWhenAllowed(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		allowed  []string
	}{
		{name: "list", toolName: "agent.list", allowed: []string{"agent.list"}},
		{name: "create", toolName: "agent.create", allowed: []string{"agent.create"}},
		{name: "switch", toolName: "agent.switch", allowed: []string{"agent.switch"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := "```json\n{\"tool_name\":\"" + tc.toolName + "\",\"arguments\":{\"agent_id\":\"default\"}}\n```"
			calls, _ := ParseToolCalls(text, tc.allowed)
			if len(calls) != 1 {
				t.Fatalf("expected one tool call, got %d", len(calls))
			}
			if calls[0].Name != tc.toolName {
				t.Fatalf("expected %s, got %q", tc.toolName, calls[0].Name)
			}
		})
	}
}

func TestParseToolCallsAcceptsRunCancelWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"run.cancel\",\"arguments\":{\"run_id\":\"run_123\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"run.cancel"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "run.cancel" {
		t.Fatalf("expected run.cancel, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsPolicyGrantWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"policy.grant\",\"arguments\":{\"agent_id\":\"worker\",\"capability\":\"fs.read\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"policy.grant"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "policy.grant" {
		t.Fatalf("expected policy.grant, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAcceptsMetricsGetWhenAllowed(t *testing.T) {
	text := "```json\n{\"tool_name\":\"metrics.get\",\"arguments\":{\"agent_id\":\"default\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"metrics.get"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "metrics.get" {
		t.Fatalf("expected metrics.get, got %q", calls[0].Name)
	}
}

func TestParseToolCallsCanonicalizesSkillGetAlias(t *testing.T) {
	text := "```json\n{\"tool_name\":\"skill.get\",\"arguments\":{\"name\":\"perplexity\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"skill.read"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "skill.read" {
		t.Fatalf("expected canonical skill.read, got %q", calls[0].Name)
	}
}

func TestParseToolCallsCanonicalizesNetFetchAlias(t *testing.T) {
	text := "```json\n{\"tool_name\":\"net.fetch\",\"arguments\":{\"url\":\"https://example.com\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"http.request"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "http.request" {
		t.Fatalf("expected canonical http.request, got %q", calls[0].Name)
	}
}

func TestParseToolCallsCapsReturnedCallsAtSix(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		b.WriteString("```json\n")
		b.WriteString(fmt.Sprintf(`{"tool_name":"fs.list","arguments":{"path":"./%d"}}`, i))
		b.WriteString("\n```\n")
	}
	calls, diag := ParseToolCalls(b.String(), []string{"fs.list"})
	if len(calls) != 6 {
		t.Fatalf("expected 6 capped tool calls, got %d", len(calls))
	}
	if len(diag.Candidates) != 8 {
		t.Fatalf("expected diagnostics for all 8 candidates, got %d", len(diag.Candidates))
	}
}

func TestParseToolCallsDiagnosticsIncludeRejectedReasons(t *testing.T) {
	text := strings.Join([]string{
		"```json\n{\"tool_name\":\"fs.list\"}\n```",
		"```json\n{invalid}\n```",
		"```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```",
	}, "\n")
	_, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(diag.Rejected) != 3 {
		t.Fatalf("expected 3 rejected candidates, got %d", len(diag.Rejected))
	}
	joined := ""
	for _, item := range diag.Rejected {
		joined += " " + strings.ToLower(item.Reason)
	}
	if !strings.Contains(joined, "missing arguments field") {
		t.Fatalf("expected missing arguments reason, got %q", joined)
	}
	if !strings.Contains(joined, "invalid json") {
		t.Fatalf("expected invalid json reason, got %q", joined)
	}
	if !strings.Contains(joined, "not allowed") {
		t.Fatalf("expected not allowed reason, got %q", joined)
	}
}

func TestParseToolCallsRejectsMissingArgumentsField(t *testing.T) {
	text := `{"tool_name":"fs.list"}`
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 0 {
		t.Fatalf("expected no accepted calls, got %d", len(calls))
	}
	if len(diag.Rejected) != 1 {
		t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
	}
	if diag.Rejected[0].Reason != "missing arguments field" {
		t.Fatalf("expected missing-arguments reason, got %q", diag.Rejected[0].Reason)
	}
}

func TestParseToolCallsRejectsNonObjectArguments(t *testing.T) {
	tests := []struct {
		name     string
		jsonText string
	}{
		{name: "null", jsonText: `{"tool_name":"fs.list","arguments":null}`},
		{name: "string", jsonText: `{"tool_name":"fs.list","arguments":"."}`},
		{name: "array", jsonText: `{"tool_name":"fs.list","arguments":[]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls, diag := ParseToolCalls(tc.jsonText, []string{"fs.list"})
			if len(calls) != 0 {
				t.Fatalf("expected no accepted calls, got %d", len(calls))
			}
			if len(diag.Rejected) != 1 {
				t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
			}
			if diag.Rejected[0].Reason != "arguments must be a JSON object" {
				t.Fatalf("unexpected rejection reason: %q", diag.Rejected[0].Reason)
			}
		})
	}
}

func TestParseToolCallsRepairsTrailingCommaJSON(t *testing.T) {
	text := "```json\nI will call a tool now.\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\",},}\nDone.\n```"
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call after repair, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", calls[0].Name)
	}
	if len(diag.Rejected) != 0 {
		t.Fatalf("expected no rejected candidates, got %#v", diag.Rejected)
	}
}

func TestParseToolCallsInvalidJSONHasDebugReason(t *testing.T) {
	text := "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":.}}\n```"
	_, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(diag.Rejected) != 1 {
		t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
	}
	reason := diag.Rejected[0].Reason
	if !strings.Contains(reason, "invalid JSON:") {
		t.Fatalf("expected detailed invalid JSON reason, got %q", reason)
	}
	if !strings.Contains(reason, "invalid character") && !strings.Contains(reason, "repair failed:") {
		t.Fatalf("expected parse detail in rejection reason, got %q", reason)
	}
}

func TestParseToolCallsRepairStillEnforcesAllowlist(t *testing.T) {
	text := "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\",},}\n```"
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 0 {
		t.Fatalf("expected no calls for disallowed tool, got %d", len(calls))
	}
	if len(diag.Rejected) != 1 {
		t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
	}
	reason := strings.ToLower(diag.Rejected[0].Reason)
	if !strings.Contains(reason, "not allowed") {
		t.Fatalf("expected allowlist rejection, got %q", diag.Rejected[0].Reason)
	}
	if strings.Contains(reason, "invalid json") {
		t.Fatalf("expected repaired JSON to pass parser before allowlist check, got %q", diag.Rejected[0].Reason)
	}
}

func TestParseToolCallsRepairsTruncatedJSON(t *testing.T) {
	// closeOpenDelimiters should close unclosed braces/brackets so the parser can extract a valid call.
	cases := []struct {
		name  string
		input string
	}{
		{"unclosed_object", "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"ls\"\n```"},
		{"unclosed_string", "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"ls -la /tmp\n```"},
		{"unclosed_nested", "```json\n{\"tool_name\":\"fs.write\",\"arguments\":{\"path\":\"/a.txt\",\"content\":\"hello\n```"},
		{"unclosed_array_arg", "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"bash\",\"args\":[\"-lc\",\"echo hi\"\n```"},
	}

	allowed := []string{"shell.exec", "fs.write"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, diag := ParseToolCalls(tc.input, allowed)
			if len(calls) == 0 {
				reasons := ""
				for _, r := range diag.Rejected {
					reasons += fmt.Sprintf("%s: %s; ", r.ParsedToolName, r.Reason)
				}
				t.Fatalf("expected truncated JSON to be repaired and parsed, got 0 calls; rejected: %s", reasons)
			}
			if calls[0].Name != "shell.exec" && calls[0].Name != "fs.write" {
				t.Fatalf("expected repaired tool name, got %q", calls[0].Name)
			}
		})
	}
}

func TestCloseOpenDelimitersUnit(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"already_closed", `{"a":1}`, `{"a":1}`},
		{"unclosed_brace", `{"a":1`, `{"a":1}`},
		{"unclosed_nested", `{"a":{"b":2}`, `{"a":{"b":2}}`},
		{"unclosed_array", `[1,2,3`, `[1,2,3]`},
		{"unclosed_string_and_brace", `{"a":"hello`, `{"a":"hello"}`},
		{"unclosed_mixed", `{"a":[1,{"b":2`, `{"a":[1,{"b":2}]}`},
		{"no_json_start", `hello world`, `hello world`},
		{"empty", ``, ``},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := closeOpenDelimiters(tc.input)
			if got != tc.expected {
				t.Fatalf("closeOpenDelimiters(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
