package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/model"
)

func TestInitCreatesStateAndConfig(t *testing.T) {
	dir := t.TempDir()
	w := Open(dir)

	if w.Initialized() {
		t.Fatal("expected fresh dir to not be initialized")
	}

	if err := w.Init("ai-trading-hub", AgentsFromNames([]string{"claude-code", "antigravity-ide"}), []string{"planning", "coding", "documenting"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if !w.Initialized() {
		t.Fatal("expected workspace to be initialized after Init")
	}

	cfg, err := w.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig failed: %v", err)
	}
	if cfg.ProjectID != "ai-trading-hub" {
		t.Fatalf("expected project_id ai-trading-hub, got %q", cfg.ProjectID)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}

	st, err := w.ReadState()
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}
	if st.CurrentStage != "planning" {
		t.Fatalf("expected initial stage planning, got %q", st.CurrentStage)
	}
	if st.HolderAgent != "claude-code" {
		t.Fatalf("expected initial holder claude-code, got %q", st.HolderAgent)
	}
}

func TestAgentConfigBackwardCompatibleYAML(t *testing.T) {
	dir := t.TempDir()
	// Old format: agents as plain strings.
	oldYAML := "project_id: legacy\nagents:\n  - claude-code\n  - antigravity-ide\nstages:\n  - planning\n  - coding\n"
	if err := os.WriteFile(filepath.Join(dir, ".agentconfig"), []byte(oldYAML), 0o644); err != nil {
		t.Fatalf("write old .agentconfig: %v", err)
	}
	cfg, err := Open(dir).LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig failed on legacy format: %v", err)
	}
	if len(cfg.Agents) != 2 || cfg.Agents[0].Name != "claude-code" {
		t.Fatalf("unexpected agents from legacy format: %+v", cfg.Agents)
	}

	// New format: agents as objects with capabilities.
	newYAML := "project_id: modern\nagents:\n  - name: claude-code\n    description: coder\n    capabilities: [planning, coding]\n  - antigravity-ide\n"
	if err := os.WriteFile(filepath.Join(dir, ".agentconfig"), []byte(newYAML), 0o644); err != nil {
		t.Fatalf("write new .agentconfig: %v", err)
	}
	cfg, err = Open(dir).LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig failed on mixed format: %v", err)
	}
	if len(cfg.Agents) != 2 || cfg.Agents[0].Description != "coder" || len(cfg.Agents[0].Capabilities) != 2 {
		t.Fatalf("unexpected agents from new format: %+v", cfg.Agents)
	}
	if cfg.Agents[1].Name != "antigravity-ide" {
		t.Fatalf("mixed string entry not parsed: %+v", cfg.Agents[1])
	}
	if !cfg.HasAgent("antigravity-ide") || cfg.HasAgent("stranger") {
		t.Fatal("HasAgent misbehaving")
	}
}

func TestInitializedRequiresStateFileNotJustDir(t *testing.T) {
	dir := t.TempDir()
	w := Open(dir)

	// Simulate incidental activity (e.g. the debug logger) creating .ao/
	// without ever running Init — this must NOT count as initialized.
	if err := os.MkdirAll(filepath.Join(dir, aoDir, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if w.Initialized() {
		t.Fatal("expected a bare .ao/ dir without state.json to not count as initialized")
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	w := Open(dir)
	if err := w.Init("proj", AgentsFromNames([]string{"a", "b"}), nil); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}
	// Simulate a handoff having advanced state, then re-run Init.
	if err := w.WriteState(State{ProjectID: "proj", CurrentStage: "coding", HolderAgent: "b", UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}
	if err := w.Init("proj", AgentsFromNames([]string{"a", "b"}), nil); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}
	st, err := w.ReadState()
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}
	if st.CurrentStage != "coding" {
		t.Fatalf("expected Init to not clobber existing state, got stage %q", st.CurrentStage)
	}
}

func TestWriteHandoffSequenceAndState(t *testing.T) {
	dir := t.TempDir()
	w := Open(dir)
	if err := w.Init("proj", AgentsFromNames([]string{"claude-code", "antigravity-ide"}), []string{"planning", "coding", "documenting"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	env1 := model.Envelope{
		ProjectID: "proj", CurrentStage: "coding",
		SourceAgent: "claude-code", TargetAgent: "antigravity-ide",
		Payload:  model.Payload{Task: "gen diagram"},
		Metadata: model.Metadata{Timestamp: time.Now(), Version: "1.0.0"},
	}
	path1, err := w.WriteHandoff(env1)
	if err != nil {
		t.Fatalf("WriteHandoff failed: %v", err)
	}
	if filepath.Base(path1) != "001-claude-code-to-antigravity-ide.json" {
		t.Fatalf("unexpected handoff filename: %s", path1)
	}

	env2 := model.Envelope{
		ProjectID: "proj", CurrentStage: "documenting",
		SourceAgent: "antigravity-ide", TargetAgent: "claude-code",
		Payload:  model.Payload{Task: "review diagram"},
		Metadata: model.Metadata{Timestamp: time.Now(), Version: "1.0.0"},
	}
	path2, err := w.WriteHandoff(env2)
	if err != nil {
		t.Fatalf("second WriteHandoff failed: %v", err)
	}
	if filepath.Base(path2) != "002-antigravity-ide-to-claude-code.json" {
		t.Fatalf("unexpected second handoff filename: %s", path2)
	}

	st, err := w.ReadState()
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}
	if st.HolderAgent != "claude-code" || st.CurrentStage != "documenting" || st.LastTask != "review diagram" {
		t.Fatalf("unexpected state after handoffs: %+v", st)
	}

	handoffs, err := w.ListHandoffs()
	if err != nil {
		t.Fatalf("ListHandoffs failed: %v", err)
	}
	if len(handoffs) != 2 {
		t.Fatalf("expected 2 handoffs, got %d", len(handoffs))
	}
	if handoffs[0].Envelope.Payload.Task != "gen diagram" {
		t.Fatalf("expected chronological order, first task was %q", handoffs[0].Envelope.Payload.Task)
	}
}
