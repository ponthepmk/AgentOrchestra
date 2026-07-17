package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInitStatusHandoffLog(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	var out, errBuf bytes.Buffer

	if code := Run([]string{"init", "--id", "ai-trading-hub", "--agents", "claude-code,antigravity-ide", dir}, &out, &errBuf); code != 0 {
		t.Fatalf("init failed (code %d): %s", code, errBuf.String())
	}

	out.Reset()
	if code := Run([]string{"status", dir}, &out, &errBuf); code != 0 {
		t.Fatalf("status failed (code %d): %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "stage:        planning") {
		t.Fatalf("unexpected status output: %s", out.String())
	}

	out.Reset()
	if code := Run([]string{"handoff", "--from", "claude-code", "--to", "antigravity-ide", "--task", "gen diagram", "--stage", "coding", dir}, &out, &errBuf); code != 0 {
		t.Fatalf("handoff failed (code %d): %s", code, errBuf.String())
	}

	out.Reset()
	if code := Run([]string{"log", dir}, &out, &errBuf); code != 0 {
		t.Fatalf("log failed (code %d): %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "gen diagram") {
		t.Fatalf("expected log to contain handoff task, got: %s", out.String())
	}
}

func TestLogFilterFlags(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	var out, errBuf bytes.Buffer

	Run([]string{"init", "--id", "proj", "--agents", "claude-code,antigravity-ide,codex", "--stages", "planning,coding,documenting", dir}, &out, &errBuf)
	out.Reset()
	Run([]string{"handoff", "--from", "claude-code", "--to", "antigravity-ide", "--task", "task 1", "--stage", "coding", dir}, &out, &errBuf)
	out.Reset()
	Run([]string{"handoff", "--from", "antigravity-ide", "--to", "codex", "--task", "task 2", "--stage", "documenting", dir}, &out, &errBuf)

	out.Reset()
	if code := Run([]string{"log", "--stage", "coding", dir}, &out, &errBuf); code != 0 {
		t.Fatalf("log --stage failed (code %d): %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "task 1") || strings.Contains(out.String(), "task 2") {
		t.Fatalf("expected --stage filter to return only task 1, got: %s", out.String())
	}

	out.Reset()
	if code := Run([]string{"log", "--agent", "codex", dir}, &out, &errBuf); code != 0 {
		t.Fatalf("log --agent failed (code %d): %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "task 2") || strings.Contains(out.String(), "task 1") {
		t.Fatalf("expected --agent filter to return only task 2, got: %s", out.String())
	}
}

func TestWatchMissingRequiredFlags(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	var out, errBuf bytes.Buffer
	Run([]string{"init", "--id", "proj", dir}, &out, &errBuf)
	out.Reset()
	errBuf.Reset()
	if code := Run([]string{"watch", "--from", "claude-code", dir}, &out, &errBuf); code == 0 {
		t.Fatal("expected non-zero exit code when --to is missing")
	}
}

func TestWatchRejectsNonexistentDir(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	var out, errBuf bytes.Buffer
	if code := Run([]string{"watch", "--from", "a", "--to", "b", "/nonexistent/path/does-not-exist"}, &out, &errBuf); code == 0 {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	var out, errBuf bytes.Buffer
	if code := Run([]string{"bogus"}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit code 2 for unknown command, got %d", code)
	}
}

func TestHandoffMissingRequiredFlags(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	var out, errBuf bytes.Buffer
	Run([]string{"init", "--id", "proj", dir}, &out, &errBuf)
	out.Reset()
	errBuf.Reset()
	if code := Run([]string{"handoff", "--from", "a", dir}, &out, &errBuf); code == 0 {
		t.Fatal("expected non-zero exit code when --to/--task are missing")
	}
}
