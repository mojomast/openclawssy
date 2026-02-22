package toolparse

import (
	"reflect"
	"testing"
)

func TestExtractBalancedJSONCandidates(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		// Basic cases
		{
			name:     "Empty",
			input:    "",
			expected: nil,
		},
		{
			name:     "Whitespace",
			input:    "   \n  \t",
			expected: nil,
		},
		{
			name:     "Simple Object",
			input:    `{"a": 1}`,
			expected: []string{`{"a": 1}`},
		},
		{
			name:     "Simple Array",
			input:    `[1, 2]`,
			expected: []string{`[1, 2]`},
		},

		// Surrounding text
		{
			name:     "Surrounding Text",
			input:    `prefix {"a": 1} suffix`,
			expected: []string{`{"a": 1}`},
		},
		{
			name:     "JSON inside markdown",
			input:    "Here is the JSON:\n```json\n{\"foo\": \"bar\"}\n```",
			expected: []string{`{"foo": "bar"}`},
		},

		// Multiple candidates
		{
			name:     "Multiple Candidates",
			input:    `Here is one: {"a": 1} and another: {"b": 2}`,
			expected: []string{`{"a": 1}`, `{"b": 2}`},
		},
		{
			name:     "Mixed Object and Array",
			input:    `Start {"a": 1} Middle [1, 2] End`,
			expected: []string{`{"a": 1}`, `[1, 2]`},
		},

		// Nesting
		{
			name:     "Nested Object",
			input:    `{"a": {"b": 2}}`,
			expected: []string{`{"a": {"b": 2}}`},
		},
		{
			name:     "Nested Array",
			input:    `[1, [2, 3], 4]`,
			expected: []string{`[1, [2, 3], 4]`},
		},
		{
			name:     "Mixed Nesting Object with Array",
			input:    `{"a": [1, 2]}`,
			expected: []string{`{"a": [1, 2]}`},
		},
		{
			name:     "Mixed Nesting Array with Object",
			input:    `{"a": [1, {"b": 2}]}`,
			expected: []string{`{"a": [1, {"b": 2}]}`},
		},
		{
			name:     "Deeply Nested",
			input:    `{"a": {"b": {"c": {"d": 1}}}}`,
			expected: []string{`{"a": {"b": {"c": {"d": 1}}}}`},
		},

		// Strings with special characters
		{
			name:     "Braces in String",
			input:    `{"msg": "{hello}"}`,
			expected: []string{`{"msg": "{hello}"}`},
		},
		{
			name:     "Brackets in String",
			input:    `{"msg": "open [ and close ]"}`,
			expected: []string{`{"msg": "open [ and close ]"}`},
		},
		{
			name:     "Escaped Quotes",
			input:    `{"msg": "say \"hello\""}`,
			expected: []string{`{"msg": "say \"hello\""}`},
		},
		{
			name:     "Escaped Backslash",
			input:    `{"path": "C:\\Windows"}`,
			expected: []string{`{"path": "C:\\Windows"}`},
		},
		{
			name:     "Escaped Backslash Before Quote",
			input:    `{"path": "ends with backslash \\"}`,
			expected: []string{`{"path": "ends with backslash \\"}`},
		},

		// Edge cases / Malformed
		{
			name:     "Incomplete JSON",
			input:    `{"a": 1`,
			expected: []string{},
		},
		{
			name:     "Extra Closing Brace",
			input:    `{"a": 1}}`,
			expected: []string{`{"a": 1}`},
		},
		{
			name:     "Mismatched Types Rejected",
			input:    `{"a": [1}}`,
			expected: []string{}, // stack-based parser rejects mismatched bracket/brace
		},
		{
			name:     "Interleaved Brackets Braces Rejected",
			input:    `{[}]`,
			expected: []string{}, // stack-based parser rejects mismatched bracket/brace
		},
		{
			name:     "Quote Inside String Without Escape",
			input:    `{"key": "va"lue"}`,
			expected: []string{},
		},
		{
			name:     "Brace Immediately After Quote",
			input:    `{"key": "value"}` + "}",
			expected: []string{`{"key": "value"}`},
		},
		{
			name:     "Ignore Partial Balanced Braces",
			input:    `text { partial } text`,
			expected: []string{`{ partial }`},
		},
		{
			name:     "Ignore Partial Balanced Brackets",
			input:    `text [ loose ] text`,
			expected: []string{`[ loose ]`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractBalancedJSONCandidates(tc.input)
			if len(got) == 0 && len(tc.expected) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("extractBalancedJSONCandidates(%q) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}
