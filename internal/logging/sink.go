package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const DefaultMaxEntries = 500

// Entry is a single captured log record.
type Entry struct {
	Timestamp time.Time
	Level     slog.Level
	Message   string
}

// MemorySink is an slog.Handler that buffers recent log entries in memory for
// display in the TUI log panel. It is safe for concurrent use.
type MemorySink struct {
	mu      sync.Mutex
	entries []Entry
	maxSize int
	level   slog.Leveler
}

// NewMemorySink creates a MemorySink that retains at most maxSize entries.
func NewMemorySink(maxSize int) *MemorySink {
	if maxSize <= 0 {
		maxSize = DefaultMaxEntries
	}
	return &MemorySink{maxSize: maxSize, level: slog.LevelDebug}
}

// Enabled implements slog.Handler.
func (s *MemorySink) Enabled(_ context.Context, level slog.Level) bool {
	return level >= s.level.Level()
}

// Handle implements slog.Handler.
func (s *MemorySink) Handle(_ context.Context, r slog.Record) error {
	var extras []string
	r.Attrs(func(a slog.Attr) bool {
		extras = append(extras, a.Key+"="+fmt.Sprintf("%v", a.Value.Any()))
		return true
	})

	msg := r.Message
	if len(extras) > 0 {
		msg += " " + strings.Join(extras, " ")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, Entry{
		Timestamp: r.Time,
		Level:     r.Level,
		Message:   msg,
	})
	if len(s.entries) > s.maxSize {
		s.entries = s.entries[len(s.entries)-s.maxSize:]
	}
	return nil
}

// WithAttrs implements slog.Handler. Returns the same sink (attrs are not
// pre-rendered; they appear per-record via Handle).
func (s *MemorySink) WithAttrs(_ []slog.Attr) slog.Handler { return s }

// WithGroup implements slog.Handler.
func (s *MemorySink) WithGroup(_ string) slog.Handler { return s }

// Entries returns a snapshot of all buffered entries.
func (s *MemorySink) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Entry, len(s.entries))
	copy(result, s.entries)
	return result
}

// Render formats all entries as a newline-separated string suitable for display
// in a TUI viewport. Lines longer than width-2 are truncated.
func (s *MemorySink) Render(width int) string {
	entries := s.Entries()
	if len(entries) == 0 {
		return "(no log entries yet)"
	}
	var b strings.Builder
	for _, e := range entries {
		line := fmt.Sprintf("%s [%s] %s",
			e.Timestamp.Format("15:04:05"),
			levelLabel(e.Level),
			e.Message,
		)
		if width > 10 && len(line) > width-2 {
			line = line[:width-5] + "..."
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func levelLabel(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DBG"
	case l < slog.LevelWarn:
		return "INF"
	case l < slog.LevelError:
		return "WRN"
	default:
		return "ERR"
	}
}
