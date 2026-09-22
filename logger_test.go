package bitquery

import (
	"fmt"
	"strings"
	"testing"
)

type recordedLog struct {
	level string
	msg   string
	args  []any
}

type recordingLogger struct{ calls []recordedLog }

func (l *recordingLogger) Debug(msg string, args ...any) { l.add("debug", msg, args) }
func (l *recordingLogger) Info(msg string, args ...any)  { l.add("info", msg, args) }
func (l *recordingLogger) Warn(msg string, args ...any)  { l.add("warn", msg, args) }
func (l *recordingLogger) Error(msg string, args ...any) { l.add("error", msg, args) }
func (l *recordingLogger) add(level, msg string, args []any) {
	l.calls = append(l.calls, recordedLog{level: level, msg: msg, args: args})
}

func TestSlogAdapterRedactsEveryLevel(t *testing.T) {
	sink := &recordingLogger{}
	logger := SlogAdapter{Log: sink}
	args := []any{
		"url", "wss://example.test/graphql?token=TOP_SECRET",
		map[string]any{"client_secret": "CLIENT_SECRET", "nested": map[string]any{"token": "NESTED_SECRET"}},
	}

	logger.Debug("debug", args...)
	logger.Info("info", args...)
	logger.Warn("warn", args...)
	logger.Error("error", args...)

	if len(sink.calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(sink.calls))
	}
	for _, call := range sink.calls {
		if call.level == "" || len(call.args) != len(args) {
			t.Fatalf("invalid log call: %+v", call)
		}
		text := fmt.Sprint(call.args)
		for _, secret := range []string{"TOP_SECRET", "CLIENT_SECRET", "NESTED_SECRET"} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s leaked through %s: %s", secret, call.level, text)
			}
		}
	}
}

func TestLoggerHelpersAcceptNilAndNop(t *testing.T) {
	if _, ok := loggerOrNop(nil).(NopLogger); !ok {
		t.Fatal("nil logger must become NopLogger")
	}

	var logger Logger = NopLogger{}
	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")
}
