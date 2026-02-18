package toolparse

import (
	"reflect"
	"testing"
)

func TestExtractBalancedJSONCandidates(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		// Basic cases
		{
			name:     "empty string",
			content:  "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			content:  "   \n\t  ",
			expected: nil,
		},
		{
			name:     "simple object",
			content:  `{"foo": "bar"}`,
			expected: []string{`{"foo": "bar"}`},
		},
		{
			name:     "simple array",
			content:  `["foo", "bar"]`,
			expected: []string{`["foo", "bar"]`},
		},

		// Surrounding text
		{
			name:     "surrounded by text",
			content:  `some text {"foo": "bar"} more text`,
			expected: []string{`{"foo": "bar"}`},
		},
		{
			name:     "surrounded by markdown",
			content:  "Here is the json:\n```json\n{\"foo\": \"bar\"}\n```",
			expected: []string{`{"foo": "bar"}`},
		},

		// Multiple candidates
		{
			name:     "multiple objects",
			content:  `{"a": 1} and {"b": 2}`,
			expected: []string{`{"a": 1}`, `{"b": 2}`},
		},
		{
			name:     "mixed object and array",
			content:  `Start {"a": 1} Middle [1, 2] End`,
			expected: []string{`{"a": 1}`, `[1, 2]`},
		},

		// Nesting
		{
			name:     "nested object",
			content:  `{"a": {"b": "c"}}`,
			expected: []string{`{"a": {"b": "c"}}`},
		},
		{
			name:     "nested array",
			content:  `[1, [2, 3], 4]`,
			expected: []string{`[1, [2, 3], 4]`},
		},
		{
			name:     "mixed nesting",
			content:  `{"a": [1, {"b": 2}]}`,
			expected: []string{`{"a": [1, {"b": 2}]}`},
		},

		// Strings with special characters
		{
			name:     "braces in string",
			content:  `{"msg": "open { and close }"}`,
			expected: []string{`{"msg": "open { and close }"}`},
		},
		{
			name:     "brackets in string",
			content:  `{"msg": "open [ and close ]"}`,
			expected: []string{`{"msg": "open [ and close ]"}`},
		},
		{
			name:     "escaped quotes",
			content:  `{"msg": "escaped \"quote\""}`,
			expected: []string{`{"msg": "escaped \"quote\""}`},
		},
		{
			name:     "escaped backslash",
			content:  `{"path": "C:\\Windows"}`,
			expected: []string{`{"path": "C:\\Windows"}`},
		},
		{
			name:     "escaped backslash before quote",
			content:  `{"path": "ends with backslash \\"}`,
			expected: []string{`{"path": "ends with backslash \\"}`},
		},

		// Edge cases / Malformed
		{
			name:     "incomplete json",
			content:  `{"a": 1`,
			expected: []string{},
		},
		{
			name:     "extra closing brace",
			content:  `{"a": 1}}`,
			expected: []string{`{"a": 1}`}, // It extracts the first valid balanced block
		},
		{
			name:     "loose matching brackets/braces",
			content:  `{"a": [1}}`, // Invalid JSON but balanced depth count
			expected: []string{`{"a": [1}}`},
		},
		{
			name:     "interleaved brackets/braces",
			content:  `{[}]`, // Invalid JSON but balanced depth count
			expected: []string{`{[}]`},
		},
		{
			name:     "quote inside string without escape (invalid json but parser behavior check)",
			content:  `{"key": "va"lue"}`,
			// Parser sees: { (d=1), " (start str), " (end str), l (ignored), u (ignored), e (ignored), " (start str), } (ignored in str), " (end str) ... wait
			// Let's trace:
			// { -> d=1
			// " -> inString=true
			// v, a
			// " -> inString=false
			// l, u, e
			// " -> inString=true
			// } -> inside string, ignored
			// " -> inString=false
			// So it won't close if there is no closing brace outside string.
			// But if we have `{"key": "va"lue"}` and then `}` outside?
			// The test string `{"key": "va"lue"}` doesn't have a closing brace outside string logic if parsed this way.
			// Actually `}` is inside the second string.
			// So it returns nothing.
			expected: []string{},
		},
        {
            name: "brace immediately after quote",
            content: `{"key": "value"}` + "}",
            expected: []string{`{"key": "value"}`},
        },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBalancedJSONCandidates(tt.content)
			if tt.expected == nil && got != nil {
				t.Errorf("expected nil, got %v", got)
			} else if tt.expected != nil && got == nil {
				// empty slice vs nil is fine if we treat them as equal, but here we expect specific behavior
				// verify if got is empty slice
				if len(got) != 0 {
					t.Errorf("expected %v, got nil", tt.expected)
				}
			} else if !reflect.DeepEqual(got, tt.expected) {
                // Handle empty slice vs nil mismatch if content is not nil
                if len(tt.expected) == 0 && len(got) == 0 {
                    return
                }
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
