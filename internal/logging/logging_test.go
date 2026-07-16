package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWritesJSONLines(t *testing.T) {
	dir := t.TempDir()
	logger, closeLog := Open(dir)
	logger.Info("test event", "foo", "bar")
	closeLog()

	data, err := os.ReadFile(filepath.Join(dir, ".ao", "logs", "ao.log"))
	if err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}
	if !strings.Contains(string(data), "test event") || !strings.Contains(string(data), `"foo":"bar"`) {
		t.Fatalf("unexpected log content: %s", data)
	}
}

func TestOpenAppends(t *testing.T) {
	dir := t.TempDir()

	l1, c1 := Open(dir)
	l1.Info("first")
	c1()

	l2, c2 := Open(dir)
	l2.Info("second")
	c2()

	data, err := os.ReadFile(filepath.Join(dir, ".ao", "logs", "ao.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "first") || !strings.Contains(string(data), "second") {
		t.Fatalf("expected both entries to be present, got: %s", data)
	}
}

func TestRotateIfLarge(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, ".ao", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(logDir, logFileName)
	big := make([]byte, maxLogBytes+1)
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatalf("write big file: %v", err)
	}

	logger, closeLog := Open(dir)
	logger.Info("after rotation")
	closeLog()

	backup := path + ".1"
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected rotated backup file to exist: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rotated log: %v", err)
	}
	if !strings.Contains(string(data), "after rotation") {
		t.Fatalf("expected fresh log file after rotation, got: %s", data)
	}
	if len(data) >= maxLogBytes {
		t.Fatalf("expected small fresh log file after rotation, got %d bytes", len(data))
	}
}
