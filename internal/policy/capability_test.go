package policy

import (
	"testing"
)

func TestCanonicalCapability(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "Exact match",
			input:    "fs.read",
			expected: "fs.read",
		},
		{
			name:     "Case insensitivity",
			input:    "FS.Read",
			expected: "fs.read",
		},
		{
			name:     "Whitespace padding",
			input:    "  fs.read  ",
			expected: "fs.read",
		},
		{
			name:     "Alias resolution",
			input:    "fs.rename",
			expected: "fs.move",
		},
		{
			name:     "Alias with case/whitespace",
			input:    "  FS.Rename  ",
			expected: "fs.move",
		},
		{
			name:     "Unknown tool",
			input:    "unknown.tool",
			expected: "unknown.tool",
		},
		{
			name:     "Unknown tool with case/whitespace",
			input:    "  Unknown.Tool  ",
			expected: "unknown.tool",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalCapability(tc.input)
			if got != tc.expected {
				t.Errorf("CanonicalCapability(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
