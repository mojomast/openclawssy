package chatstore

import (
	"bytes"
	"strings"
	"testing"
)

func TestReverseScanner(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		bufSize int
		want    []string
	}{
		{
			name:    "empty",
			input:   "",
			bufSize: 1024,
			want:    nil,
		},
		{
			name:    "one line",
			input:   "hello",
			bufSize: 1024,
			want:    []string{"hello"},
		},
		{
			name:    "one line with newline",
			input:   "hello\n",
			bufSize: 1024,
			want:    []string{"hello"},
		},
		{
			name:    "two lines",
			input:   "one\ntwo",
			bufSize: 1024,
			want:    []string{"two", "one"},
		},
		{
			name:    "two lines with newline",
			input:   "one\ntwo\n",
			bufSize: 1024,
			want:    []string{"two", "one"},
		},
		{
			name:    "three lines",
			input:   "1\n2\n3",
			bufSize: 1024,
			want:    []string{"3", "2", "1"},
		},
		{
			name:    "empty lines",
			input:   "a\n\nb",
			bufSize: 1024,
			want:    []string{"b", "", "a"},
		},
		{
			name:    "CRLF",
			input:   "a\r\nb",
			bufSize: 1024,
			want:    []string{"b", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader([]byte(tt.input))
			s, err := NewReverseScanner(r, tt.bufSize, 1024*1024)
			if err != nil {
				t.Fatalf("NewReverseScanner: %v", err)
			}

			var got []string
			for s.Scan() {
				got = append(got, s.Text())
			}
			if err := s.Err(); err != nil {
				t.Fatalf("Scan error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Errorf("got %d lines, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if i < len(tt.want) && got[i] != tt.want[i] {
					t.Errorf("line %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestReverseScannerChunks(t *testing.T) {
	// 5000 'a's.
	longLine := strings.Repeat("a", 5000)
	// input: longLine \n longLine
	input := longLine + "\n" + longLine

	r := bytes.NewReader([]byte(input))
	// NewReverseScanner clamps to 4096. So reading 5000 chars will require 2 chunks per line (roughly).
	s, err := NewReverseScanner(r, 4096, 10000)
	if err != nil {
		t.Fatalf("NewReverseScanner: %v", err)
	}

	var got []string
	for s.Scan() {
		got = append(got, s.Text())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2", len(got))
	}
	if got[0] != longLine {
		t.Errorf("line 0 mismatch length %d", len(got[0]))
	}
	if got[1] != longLine {
		t.Errorf("line 1 mismatch length %d", len(got[1]))
	}
}
