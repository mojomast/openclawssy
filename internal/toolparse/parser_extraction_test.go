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
			name:     "Mixed Nesting",
			input:    `{"a": [1, 2]}`,
			expected: []string{`{"a": [1, 2]}`},
		},
		{
			name:     "Braces in String",
			input:    `{"msg": "{hello}"}`,
			expected: []string{`{"msg": "{hello}"}`},
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
			name:     "Multiple Candidates",
			input:    `Here is one: {"a": 1} and another: {"b": 2}`,
			expected: []string{`{"a": 1}`, `{"b": 2}`},
		},
		{
			name:     "Surrounding Text",
			input:    `prefix {"a": 1} suffix`,
			expected: []string{`{"a": 1}`},
		},
		{
			name:     "Incomplete JSON",
			input:    `{"a": 1`,
			expected: []string{}, // Expecting empty slice, not nil? Or nil depending on implementation. The current impl returns empty slice if candidates created but none complete? No, make() uses cap 4. But slice is empty.
		},
		{
			name:     "Malformed Mismatched",
			input:    `{[}]`,
			expected: []string{}, // Should be rejected
		},
		{
			name:     "Malformed Typo",
			input:    `{"a": [1, 2}}`,
			expected: []string{}, // Should be rejected
		},
		{
			name:     "Deeply Nested",
			input:    `{"a": {"b": {"c": {"d": 1}}}}`,
			expected: []string{`{"a": {"b": {"c": {"d": 1}}}}`},
		},
		{
			name:     "Ignore Partial",
			input:    `text { partial } text`,
			expected: []string{`{ partial }`}, // This is balanced so it extracts.
		},
		{
			name:     "Loose Brackets",
			input:    `text [ loose ] text`,
			expected: []string{`[ loose ]`}, // Balanced, extracts.
		},
		{
			name:     "JSON inside markdown",
			input:    "Here is the JSON:\n```json\n{\"foo\": \"bar\"}\n```",
			expected: []string{`{"foo": "bar"}`},
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
