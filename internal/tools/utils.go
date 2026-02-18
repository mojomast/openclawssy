package tools

import (
	"fmt"
	"strconv"
	"strings"
)

// parseInt robustly parses an integer from any value.
// It handles nil, strings, and other types via fmt.Sprintf.
// Returns fallback if parsing fails.
func parseInt(raw any, fallback int) int {
	if raw == nil {
		return fallback
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if s == "" || s == "<nil>" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

// getIntArg retrieves an integer argument from a map, using parseInt.
func getIntArg(args map[string]any, key string, fallback int) int {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	return parseInt(v, fallback)
}

// getString retrieves a string argument from a map.
func getString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument must be string: %s", key)
	}
	return s, nil
}

// getBoolArg retrieves a boolean argument from a map.
func getBoolArg(args map[string]any, key string, fallback bool) bool {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

// valueString safely retrieves a string argument from a map, returning empty string if missing or invalid.
func valueString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}
