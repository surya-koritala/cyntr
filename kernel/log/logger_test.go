package log

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLoggerInfo(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, INFO, "test")
	l.Info("hello")

	var entry Entry
	json.Unmarshal(buf.Bytes(), &entry)
	if entry.Level != "INFO" {
		t.Fatalf("got %q", entry.Level)
	}
	if entry.Message != "hello" {
		t.Fatalf("got %q", entry.Message)
	}
	if entry.Module != "test" {
		t.Fatalf("got %q", entry.Module)
	}
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, INFO, "")
	l.Info("request", map[string]any{"tenant": "finance", "status": 200})

	var entry Entry
	json.Unmarshal(buf.Bytes(), &entry)
	if entry.Fields["tenant"] != "finance" {
		t.Fatalf("missing field")
	}
}

func TestLoggerLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, WARN, "")
	l.Debug("ignored")
	l.Info("ignored")
	l.Warn("visible")

	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
	if bytes.Contains(buf.Bytes(), []byte("ignored")) {
		t.Fatal("debug/info should be filtered")
	}
}

func TestLoggerWithModule(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, INFO, "parent")
	child := l.WithModule("child")
	child.Info("test")

	var entry Entry
	json.Unmarshal(buf.Bytes(), &entry)
	if entry.Module != "child" {
		t.Fatalf("got %q", entry.Module)
	}
}

func TestLevelString(t *testing.T) {
	if DEBUG.String() != "DEBUG" {
		t.Fatal()
	}
	if INFO.String() != "INFO" {
		t.Fatal()
	}
	if WARN.String() != "WARN" {
		t.Fatal()
	}
	if ERROR.String() != "ERROR" {
		t.Fatal()
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	// Just verify they don't panic
	var buf bytes.Buffer
	defaultLogger = New(&buf, DEBUG, "")
	Debug("d")
	Info("i")
	Warn("w")
	Error("e")
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}
