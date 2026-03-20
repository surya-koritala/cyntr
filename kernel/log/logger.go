package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity.
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

// Entry is a structured log entry.
type Entry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Module  string         `json:"module,omitempty"`
	Message string         `json:"msg"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Logger provides structured logging.
type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	level  Level
	module string
}

var defaultLogger = New(os.Stderr, INFO, "")

// New creates a logger.
func New(out io.Writer, level Level, module string) *Logger {
	return &Logger{out: out, level: level, module: module}
}

// Default returns the default logger.
func Default() *Logger { return defaultLogger }

// SetLevel sets the minimum log level.
func SetLevel(l Level) { defaultLogger.level = l }

// WithModule returns a new logger with the given module name.
func (l *Logger) WithModule(module string) *Logger {
	return &Logger{out: l.out, level: l.level, module: module}
}

func (l *Logger) log(level Level, msg string, fields map[string]any) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := Entry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Level:   level.String(),
		Module:  l.module,
		Message: msg,
		Fields:  fields,
	}

	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.out, string(data))
}

func (l *Logger) Debug(msg string, fields ...map[string]any) {
	var f map[string]any
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(DEBUG, msg, f)
}

func (l *Logger) Info(msg string, fields ...map[string]any) {
	var f map[string]any
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(INFO, msg, f)
}

func (l *Logger) Warn(msg string, fields ...map[string]any) {
	var f map[string]any
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(WARN, msg, f)
}

func (l *Logger) Error(msg string, fields ...map[string]any) {
	var f map[string]any
	if len(fields) > 0 {
		f = fields[0]
	}
	l.log(ERROR, msg, f)
}

// Package-level convenience functions
func Debug(msg string, fields ...map[string]any) { defaultLogger.Debug(msg, fields...) }
func Info(msg string, fields ...map[string]any)  { defaultLogger.Info(msg, fields...) }
func Warn(msg string, fields ...map[string]any)  { defaultLogger.Warn(msg, fields...) }
func Error(msg string, fields ...map[string]any) { defaultLogger.Error(msg, fields...) }
