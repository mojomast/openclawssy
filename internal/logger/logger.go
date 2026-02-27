// Package logger initializes the global slog logger from environment variables.
//
// Call Init() once at program startup (before any log calls). After that, all
// packages can use the slog package-level functions (slog.Debug, slog.Info,
// slog.Warn, slog.Error) directly.
//
// Environment variables:
//
//	OPENCLAWSSY_LOG_LEVEL  – debug | info | warn | error  (default: info)
package logger

import (
	"log/slog"
	"os"
	"strings"
)

const EnvLogLevel = "OPENCLAWSSY_LOG_LEVEL"

// Init configures the global slog logger. It reads OPENCLAWSSY_LOG_LEVEL and
// installs a text handler writing to stderr. Call this once at the top of
// main() before any other initialisation.
func Init() {
	level := parseLevel(os.Getenv(EnvLogLevel))
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
