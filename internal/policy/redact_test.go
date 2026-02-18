package policy

import (
	"reflect"
	"testing"
)

func TestRedactString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no redaction needed",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "api-key redaction",
			input: "api-key: mysecretkey",
			want:  "[REDACTED]",
		},
		{
			name:  "token redaction with equals",
			input: "token = 1234567890",
			want:  "[REDACTED]",
		},
		{
			name:  "bearer token redaction",
			input: "Bearer abcdef123456",
			want:  "[REDACTED]",
		},
		{
			name:  "uppercase token key",
			input: "MY_TOKEN = secretvalue",
			want:  "MY_[REDACTED]", // Matches "TOKEN = secretvalue" via first rule
		},
		{
			name:  "long random string",
			input: "abcdefghijklmnopqrstuvwxyz123456",
			want:  "[REDACTED]",
		},
		{
			name:  "multiple redactions",
			input: "api-key: key1 and token = token2",
			want:  "[REDACTED] and [REDACTED]",
		},
		{
			name:  "case insensitivity",
			input: "API-KEY: key",
			want:  "[REDACTED]",
		},
		{
			name:  "password redaction",
			input: "password = supersecret",
			want:  "[REDACTED]",
		},
		// JSON keys with quotes are NOT supported by current regex unless the key itself matches without quotes being part of the match?
		// "api_key": "secret" -> key is "api_key".
		// Regex expects key followed by : or =.
		// "api_key": -> key is "api_key".
		// The regex (api...key) matches api_key.
		// The next char is ". Regex expects : or =. So no match.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactString(tc.input)
			if got != tc.want {
				t.Errorf("RedactString(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{
			name:  "string input",
			input: "api-key: secret",
			want:  "[REDACTED]",
		},
		{
			name:  "int input",
			input: 123,
			want:  123,
		},
		{
			name:  "bool input",
			input: true,
			want:  true,
		},
		{
			name: "map input",
			input: map[string]any{
				"key":    "value",
				"secret": "api-key: 12345",
			},
			want: map[string]any{
				"key":    "value",
				"secret": "[REDACTED]",
			},
		},
		{
			name:  "slice input",
			input: []any{"safe", "token = 123"},
			want:  []any{"safe", "[REDACTED]"},
		},
		{
			name: "nested map in slice",
			input: []any{
				map[string]any{"k": "v"},
				map[string]any{"s": "password = 123"},
			},
			want: []any{
				map[string]any{"k": "v"},
				map[string]any{"s": "[REDACTED]"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactValue(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("RedactValue(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
