package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupConfigPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
	t.Setenv("AO_CLAUDE_DESKTOP_CONFIG", path)
	return path
}

func readServers(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	return servers
}

func TestInstallIntoFreshConfig(t *testing.T) {
	path := setupConfigPath(t)
	gotPath, action, err := SetupClaudeDesktop(false)
	if err != nil {
		t.Fatalf("SetupClaudeDesktop failed: %v", err)
	}
	if gotPath != path || action != "installed" {
		t.Fatalf("unexpected result: %s / %s", gotPath, action)
	}
	servers := readServers(t, path)
	entry, ok := servers["agentorchestra"].(map[string]any)
	if !ok {
		t.Fatalf("agentorchestra entry missing: %v", servers)
	}
	if entry["command"] == "" || entry["command"] == "ao" {
		t.Fatalf("expected absolute binary path, got %v", entry["command"])
	}
}

func TestMergePreservesOtherServersAndBacksUp(t *testing.T) {
	path := setupConfigPath(t)
	original := `{"mcpServers": {"other-tool": {"command": "other", "args": ["x"]}}, "theme": "dark"}`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	_, action, err := SetupClaudeDesktop(false)
	if err != nil {
		t.Fatalf("SetupClaudeDesktop failed: %v", err)
	}
	if action != "installed" {
		t.Fatalf("expected installed, got %s", action)
	}

	servers := readServers(t, path)
	if _, ok := servers["other-tool"]; !ok {
		t.Fatal("expected other-tool server to be preserved")
	}
	if _, ok := servers["agentorchestra"]; !ok {
		t.Fatal("expected agentorchestra to be added")
	}

	var cfg map[string]any
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &cfg)
	if cfg["theme"] != "dark" {
		t.Fatal("expected unrelated top-level keys to be preserved")
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if string(backup) != original {
		t.Fatal("expected backup to contain the pre-modification content")
	}
}

func TestRefusesUnparseableConfig(t *testing.T) {
	path := setupConfigPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	broken := `{"mcpServers": {,}`
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := SetupClaudeDesktop(false)
	if err == nil {
		t.Fatal("expected refusal on unparseable config")
	}
	data, _ := os.ReadFile(path)
	if string(data) != broken {
		t.Fatal("expected original file to be left untouched")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("expected no backup to be written on refusal")
	}
}

func TestRemove(t *testing.T) {
	path := setupConfigPath(t)
	if _, _, err := SetupClaudeDesktop(false); err != nil {
		t.Fatal(err)
	}
	_, action, err := SetupClaudeDesktop(true)
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if action != "removed" {
		t.Fatalf("expected removed, got %s", action)
	}
	servers := readServers(t, path)
	if _, ok := servers["agentorchestra"]; ok {
		t.Fatal("expected agentorchestra entry to be gone")
	}

	// Removing again is a clean no-op.
	_, action, err = SetupClaudeDesktop(true)
	if err != nil || action != "not installed" {
		t.Fatalf("expected idempotent remove, got %s / %v", action, err)
	}
}

func TestRemoveWhenNoConfigFile(t *testing.T) {
	setupConfigPath(t)
	_, action, err := SetupClaudeDesktop(true)
	if err != nil || action != "not installed" {
		t.Fatalf("expected clean no-op, got %s / %v", action, err)
	}
}
