// Package logger provides a configured zerolog.Logger for the application.
//
// It lives under pkg/ because it carries no Balvia-specific business logic and
// could be reused by other services.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New builds a zerolog.Logger.
//
//   - level: one of trace|debug|info|warn|error (invalid values fall back to info).
//   - pretty: when true, logs are human-readable colored console output (dev);
//     when false, logs are structured JSON (staging/production).
func New(level string, pretty bool) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339

	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	var l zerolog.Logger
	if pretty {
		out := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		l = zerolog.New(out)
	} else {
		l = zerolog.New(os.Stdout)
	}

	return l.Level(lvl).With().Timestamp().Logger()
}
