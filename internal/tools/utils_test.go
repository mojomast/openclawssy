package tools

import (
	"testing"
)

func TestParseInt(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		fallback int
		want     int
	}{
		{"nil", nil, 10, 10},
		{"int", 42, 0, 42},
		{"string int", "123", 0, 123},
		{"string int spaces", " 456 ", 0, 456},
		{"invalid string", "abc", 5, 5},
		{"float", 12.34, 0, 0}, // Sprintf("%v", 12.34) -> "12.34", Atoi("12.34") -> err
		{"float string", "12.34", 0, 0},
		{"empty string", "", 99, 99},
		{"spaces only", "   ", 99, 99},
		{"<nil> string", "<nil>", 88, 88},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInt(tt.input, tt.fallback); got != tt.want {
				t.Errorf("parseInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetIntArg(t *testing.T) {
	args := map[string]any{
		"a": 1,
		"b": "2",
		"c": nil,
		"d": "invalid",
	}

	tests := []struct {
		key      string
		fallback int
		want     int
	}{
		{"a", 0, 1},
		{"b", 0, 2},
		{"c", 10, 10},
		{"d", 20, 20},
		{"missing", 30, 30},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := getIntArg(args, tt.key, tt.fallback); got != tt.want {
				t.Errorf("getIntArg(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestGetString(t *testing.T) {
	args := map[string]any{
		"a": "hello",
		"b": 123,
	}

	tests := []struct {
		key     string
		want    string
		wantErr bool
	}{
		{"a", "hello", false},
		{"b", "", true},
		{"missing", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := getString(args, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("getString(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getString(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestGetBoolArg(t *testing.T) {
	args := map[string]any{
		"true":  true,
		"false": false,
		"str":   "true",
		"int":   1,
	}

	tests := []struct {
		key      string
		fallback bool
		want     bool
	}{
		{"true", false, true},
		{"false", true, false},
		{"str", false, false},
		{"int", true, true},
		{"missing", true, true},
		{"missing", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := getBoolArg(args, tt.key, tt.fallback); got != tt.want {
				t.Errorf("getBoolArg(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestValueString(t *testing.T) {
	args := map[string]any{
		"s":   "string",
		"n":   nil,
		"i":   123,
		"emp": "",
	}

	tests := []struct {
		key  string
		want string
	}{
		{"s", "string"},
		{"n", ""},
		{"i", ""},
		{"emp", ""},
		{"missing", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := valueString(args, tt.key); got != tt.want {
				t.Errorf("valueString(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
