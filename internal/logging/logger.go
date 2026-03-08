package logging

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

func ParseLevel(s string) (Level, bool) {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return DEBUG, true
	case "INFO":
		return INFO, true
	case "WARN":
		return WARN, true
	case "ERROR":
		return ERROR, true
	default:
		return DEBUG, false
	}
}

type Entry struct {
	ID        int               `json:"id"`
	Time      time.Time         `json:"time"`
	Level     Level             `json:"level"`
	LevelStr  string            `json:"level_str"`
	Component string            `json:"component"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Logger struct {
	mu       sync.RWMutex
	entries  []Entry
	size     int
	pos      int
	count    int
	nextID   int
	minLevel Level
}

var (
	defaultLogger *Logger
	once          sync.Once
)

func Init(bufferSize int) {
	once.Do(func() {
		defaultLogger = &Logger{
			entries:  make([]Entry, bufferSize),
			size:     bufferSize,
			nextID:   1,
			minLevel: DEBUG,
		}
	})
}

func Get() *Logger {
	if defaultLogger == nil {
		Init(2000)
	}
	return defaultLogger
}

func (l *Logger) SetMinLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

func (l *Logger) log(level Level, component, msg string, meta ...map[string]string) {
	if level < l.minLevel {
		return
	}

	entry := Entry{
		Time:      time.Now(),
		Level:     level,
		LevelStr:  level.String(),
		Component: component,
		Message:   msg,
	}
	if len(meta) > 0 && meta[0] != nil {
		entry.Metadata = meta[0]
	}

	l.mu.Lock()
	entry.ID = l.nextID
	l.nextID++
	l.entries[l.pos] = entry
	l.pos = (l.pos + 1) % l.size
	if l.count < l.size {
		l.count++
	}
	l.mu.Unlock()

	// Console output
	emoji := levelEmoji(level)
	fmt.Fprintf(os.Stderr, "%s [%s] [%s] %s\n", emoji, level, component, msg)
}

func (l *Logger) Debug(component, msg string, meta ...map[string]string) {
	l.log(DEBUG, component, msg, meta...)
}

func (l *Logger) Info(component, msg string, meta ...map[string]string) {
	l.log(INFO, component, msg, meta...)
}

func (l *Logger) Warn(component, msg string, meta ...map[string]string) {
	l.log(WARN, component, msg, meta...)
}

func (l *Logger) Error(component, msg string, meta ...map[string]string) {
	l.log(ERROR, component, msg, meta...)
}

// Query returns entries matching the filters.
// sinceID=0 returns all. Returns (matching entries, latestID).
func (l *Logger) Query(level *Level, component string, keyword string, sinceID int, limit int) ([]Entry, int) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 {
		limit = 200
	}

	// Collect all entries in chronological order
	var all []Entry
	if l.count < l.size {
		// Buffer not full yet
		all = l.entries[:l.count]
	} else {
		// Buffer is full, read from pos (oldest) wrapping around
		all = make([]Entry, l.size)
		for i := 0; i < l.size; i++ {
			all[i] = l.entries[(l.pos+i)%l.size]
		}
	}

	latestID := l.nextID - 1

	// Filter
	var result []Entry
	for _, e := range all {
		if e.ID <= sinceID {
			continue
		}
		if level != nil && e.Level < *level {
			continue
		}
		if component != "" && e.Component != component {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(e.Message), strings.ToLower(keyword)) {
			continue
		}
		result = append(result, e)
	}

	// Apply limit (take latest N)
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, latestID
}

func levelEmoji(level Level) string {
	switch level {
	case DEBUG:
		return "🔍"
	case INFO:
		return "ℹ️"
	case WARN:
		return "⚠️"
	case ERROR:
		return "❌"
	default:
		return "📝"
	}
}
