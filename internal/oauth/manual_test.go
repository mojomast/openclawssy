package oauth

import (
	"testing"
)

func TestParseManualInput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCode  string
		wantState string
		wantErr   bool
	}{
		{
			name:     "bare code string",
			input:    "abc123def456",
			wantCode: "abc123def456",
		},
		{
			name:      "full URL with code and state",
			input:     "http://127.0.0.1:12345/callback?code=the_code&state=the_state",
			wantCode:  "the_code",
			wantState: "the_state",
		},
		{
			name:     "URL with code only",
			input:    "http://localhost:8080/callback?code=only_code",
			wantCode: "only_code",
		},
		{
			name:    "empty input returns error",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only returns error",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "URL without code param returns error",
			input:   "http://localhost:8080/callback?state=nocode",
			wantErr: true,
		},
		{
			name:     "bare code with leading/trailing whitespace is trimmed",
			input:    "  trimmed_code  ",
			wantCode: "trimmed_code",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, state, err := ParseManualInput(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseManualInput(%q): expected error, got nil (code=%q)", tc.input, code)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseManualInput(%q): unexpected error: %v", tc.input, err)
			}
			if code != tc.wantCode {
				t.Errorf("code: got %q, want %q", code, tc.wantCode)
			}
			if state != tc.wantState {
				t.Errorf("state: got %q, want %q", state, tc.wantState)
			}
		})
	}
}
