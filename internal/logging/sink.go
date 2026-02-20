package logging

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/willibrandon/mtlog/core"
)

const DefaultMaxEntries = 500

// Entry is a single captured log event.
type Entry struct {
	Timestamp time.Time
	Level     core.LogEventLevel
	Message   string
}

// MemorySink is an mtlog LogEventSink that buffers recent log entries in memory.
// It is safe for concurrent use.
type MemorySink struct {
	mu      sync.Mutex
	entries []Entry
	maxSize int
}

// NewMemorySink creates a MemorySink that retains at most maxSize entries.
func NewMemorySink(maxSize int) *MemorySink {
	if maxSize <= 0 {
		maxSize = DefaultMaxEntries
	}
	return &MemorySink{maxSize: maxSize}
}

// Emit implements core.LogEventSink.
func (s *MemorySink) Emit(event *core.LogEvent) {
	msg := event.RenderMessage()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, Entry{
		Timestamp: event.Timestamp,
		Level:     event.Level,
		Message:   msg,
	})
	if len(s.entries) > s.maxSize {
		s.entries = s.entries[len(s.entries)-s.maxSize:]
	}
}

// Close implements core.LogEventSink.
func (s *MemorySink) Close() error { return nil }

// Entries returns a snapshot of all buffered entries.
func (s *MemorySink) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Entry, len(s.entries))
	copy(result, s.entries)
	return result
}

// Render formats all entries as a newline-separated string for display in a
// TUI viewport. Lines longer than width-2 are truncated.
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

func levelLabel(l core.LogEventLevel) string {
	switch l {
	case core.VerboseLevel:
		return "VRB"
	case core.DebugLevel:
		return "DBG"
	case core.InformationLevel:
		return "INF"
	case core.WarningLevel:
		return "WRN"
	case core.ErrorLevel:
		return "ERR"
	case core.FatalLevel:
		return "FTL"
	default:
		return "   "
	}
}
