package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInitStatusHandoffLog(t *testing.T) {
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

func TestUnknownCommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := Run([]string{"bogus"}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit code 2 for unknown command, got %d", code)
	}
}

func TestHandoffMissingRequiredFlags(t *testing.T) {
	dir := t.TempDir()
	var out, errBuf bytes.Buffer
	Run([]string{"init", "--id", "proj", dir}, &out, &errBuf)
	out.Reset()
	errBuf.Reset()
	if code := Run([]string{"handoff", "--from", "a", dir}, &out, &errBuf); code == 0 {
		t.Fatal("expected non-zero exit code when --to/--task are missing")
	}
}
