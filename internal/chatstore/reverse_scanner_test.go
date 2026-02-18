package chatstore

import (
	"strings"
	"testing"
)

func TestReverseScanner(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
		bufSize  int64
	}{
		{
			name:     "Empty file",
			content:  "",
			expected: []string{},
			bufSize:  10,
		},
		{
			name:     "One line",
			content:  "hello",
			expected: []string{"hello"},
			bufSize:  10,
		},
		{
			name:     "Two lines",
			content:  "first\nsecond",
			expected: []string{"second", "first"},
			bufSize:  10,
		},
		{
			name:     "Trailing newline",
			content:  "first\nsecond\n",
			expected: []string{"", "second", "first"},
			bufSize:  10,
		},
		{
			name:     "Leading newline",
			content:  "\nfirst",
			expected: []string{"first", ""},
			bufSize:  10,
		},
		{
			name:     "Multiple lines small buffer",
			content:  "1\n2\n3\n4\n5",
			expected: []string{"5", "4", "3", "2", "1"},
			bufSize:  2,
		},
		{
			name:     "Long line larger than buffer",
			content:  "start" + strings.Repeat("a", 20) + "end",
			expected: []string{"start" + strings.Repeat("a", 20) + "end"},
			bufSize:  5,
		},
		{
			name:     "Complex mixed",
			content:  "a\nb\nc\nd",
			expected: []string{"d", "c", "b", "a"},
			bufSize:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.content)

			scanner, err := NewReverseScanner(r, tt.bufSize)
			if err != nil {
				t.Fatalf("NewReverseScanner error: %v", err)
			}

			var got []string
			for scanner.Scan() {
				got = append(got, scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				t.Errorf("scanner error: %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Errorf("got %d lines, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				if i >= len(tt.expected) {
					break
				}
				if got[i] != tt.expected[i] {
					t.Errorf("line %d: got %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
