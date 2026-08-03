package openai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// requestLogEntry is one JSON line in a day's log file.
type requestLogEntry struct {
	Timestamp  time.Time     `json:"timestamp"`
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Response   string        `json:"response,omitempty"`
	Error      string        `json:"error,omitempty"`
	DurationMS int64         `json:"duration_ms"`
}

// requestLogger writes one JSON line per call to dir/<YYYY-MM-DD>.log.
// Logging is best-effort: a write failure is logged and swallowed, never
// returned to the caller - losing a log line must not fail the API call it
// describes.
type requestLogger struct {
	dir string
	log *slog.Logger
	mu  sync.Mutex
}

// newRequestLogger returns nil when dir is empty, so callers can treat a nil
// *requestLogger as "logging disabled" without a branch at every call site.
func newRequestLogger(dir string, log *slog.Logger) *requestLogger {
	if dir == "" {
		return nil
	}
	return &requestLogger{dir: dir, log: log}
}

func (l *requestLogger) write(entry requestLogEntry) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		l.warn("create log dir", err)
		return
	}

	path := filepath.Join(l.dir, entry.Timestamp.Format("2006-01-02")+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		l.warn("open log file", err)
		return
	}
	defer func() { _ = f.Close() }()

	line, err := json.Marshal(entry)
	if err != nil {
		l.warn("marshal log entry", err)
		return
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		l.warn("write log entry", err)
	}
}

func (l *requestLogger) warn(action string, err error) {
	if l.log == nil {
		return
	}
	l.log.Warn(fmt.Sprintf("openai request log: %s failed", action), slog.String("error", err.Error()))
}
