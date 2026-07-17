package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

func TestApplyCreatesAllFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, TeamPreset()); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	for _, f := range []string{".mcp.json", "CLAUDE.md", "AGENTS.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(claude), "ao_agents") || !strings.Contains(string(claude), "antigravity-ide") {
		t.Fatalf("CLAUDE.md missing expected instructions: %s", claude)
	}
	gitignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gitignore), ".ao/presence/") {
		t.Fatalf(".gitignore missing presence entry: %s", gitignore)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	agents := []workspace.Agent{{Name: "a"}, {Name: "b"}}
	if err := Apply(dir, agents); err != nil {
		t.Fatalf("first Apply failed: %v", err)
	}
	if err := Apply(dir, agents); err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if got := strings.Count(string(claude), markerBegin); got != 1 {
		t.Fatalf("expected exactly 1 AgentOrchestra section after re-apply, got %d:\n%s", got, claude)
	}
	gitignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if got := strings.Count(string(gitignore), ".ao/presence/"); got != 1 {
		t.Fatalf("expected exactly 1 presence gitignore line, got %d", got)
	}
}

func TestApplyPreservesExistingClaudeMD(t *testing.T) {
	dir := t.TempDir()
	original := "# My project\n\nคำแนะนำเดิมของโปรเจกต์\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(original), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	if err := Apply(dir, []workspace.Agent{{Name: "a"}, {Name: "b"}}); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(claude), "คำแนะนำเดิมของโปรเจกต์") {
		t.Fatal("expected existing CLAUDE.md content to be preserved")
	}
	if !strings.Contains(string(claude), markerBegin) {
		t.Fatal("expected AgentOrchestra section to be appended")
	}
}

func TestApplyNeverClobbersExistingMCPJSON(t *testing.T) {
	dir := t.TempDir()
	custom := `{"mcpServers": {"other": {}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(custom), 0o644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}
	if err := Apply(dir, nil); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if string(data) != custom {
		t.Fatal("expected existing .mcp.json to be left untouched")
	}
}
