package bitquery

import (
	"github.com/tigusigalpa/bitquery-go/internal/redact"
)

// Logger is a minimal structured logger (slog-compatible shape).
// Implementations must accept nil — the default is no-op. All output is
// redacted before reaching the sink.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// SlogAdapter wraps a *slog.Logger-compatible sink.
type SlogAdapter struct {
	Log interface {
		Debug(msg string, args ...any)
		Info(msg string, args ...any)
		Warn(msg string, args ...any)
		Error(msg string, args ...any)
	}
}

// Debug forwards a redacted debug message to the wrapped logger.
func (a SlogAdapter) Debug(msg string, args ...any) { a.Log.Debug(msg, sanitizeArgs(args)...) }

// Info forwards a redacted informational message to the wrapped logger.
func (a SlogAdapter) Info(msg string, args ...any) { a.Log.Info(msg, sanitizeArgs(args)...) }

// Warn forwards a redacted warning message to the wrapped logger.
func (a SlogAdapter) Warn(msg string, args ...any) { a.Log.Warn(msg, sanitizeArgs(args)...) }

// Error forwards a redacted error message to the wrapped logger.
func (a SlogAdapter) Error(msg string, args ...any) { a.Log.Error(msg, sanitizeArgs(args)...) }

func sanitizeArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case string:
			out[i] = redact.String(v)
		case map[string]any:
			out[i] = redact.Map(v)
		default:
			out[i] = a
		}
	}
	return out
}

// NopLogger is the default no-op logger.
type NopLogger struct{}

// Debug discards a debug message.
func (NopLogger) Debug(string, ...any) {}

// Info discards an informational message.
func (NopLogger) Info(string, ...any) {}

// Warn discards a warning message.
func (NopLogger) Warn(string, ...any) {}

// Error discards an error message.
func (NopLogger) Error(string, ...any) {}

func loggerOrNop(l Logger) Logger {
	if l == nil {
		return NopLogger{}
	}
	return l
}
