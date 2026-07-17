package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

func TestFullHandoffCycle(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)

	if err := o.Init("ai-trading-hub", workspace.AgentsFromNames([]string{"claude-code", "antigravity-ide"}), []string{"planning", "coding", "documenting"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	st, err := o.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if st.CurrentStage != "planning" || st.HolderAgent != "claude-code" {
		t.Fatalf("unexpected initial status: %+v", st)
	}

	_, err = o.Handoff(HandoffRequest{
		SourceAgent: "claude-code",
		TargetAgent: "antigravity-ide",
		Stage:       "documenting",
		Task:        "generate architecture diagram",
		Artifacts:   []string{"src/train.py"},
	})
	if err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}

	st, err = o.Status()
	if err != nil {
		t.Fatalf("Status after handoff failed: %v", err)
	}
	if st.CurrentStage != "documenting" || st.HolderAgent != "antigravity-ide" || st.LastTask != "generate architecture diagram" {
		t.Fatalf("unexpected status after handoff: %+v", st)
	}

	records, err := o.Log(LogFilter{})
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}
	if len(records) != 1 || records[0].Task != "generate architecture diagram" {
		t.Fatalf("unexpected log: %+v", records)
	}

	// Simulate a lost/corrupted index and recover via Reindex.
	if err := o.Reindex(); err != nil {
		t.Fatalf("Reindex failed: %v", err)
	}
	records, err = o.Log(LogFilter{})
	if err != nil {
		t.Fatalf("Log after reindex failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after reindex, got %d", len(records))
	}
}

func TestHandoffNotesMirrorAndAgents(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	agents := []workspace.Agent{
		{Name: "claude-code", Description: "coder", Capabilities: []string{"coding"}},
		{Name: "antigravity-ide", Description: "diagrams", Capabilities: []string{"infographic"}},
	}
	if err := o.Init("proj", agents, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := o.Handoff(HandoffRequest{
		SourceAgent: "claude-code",
		TargetAgent: "antigravity-ide",
		Task:        "gen diagram",
		Notes:       "โฟกัสที่ pipeline หลักพอ",
		Artifacts:   []string{"src/main.go"},
	})
	if err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}

	// Notes flow into status.
	st, err := o.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if st.LastNotes != "โฟกัสที่ pipeline หลักพอ" {
		t.Fatalf("expected notes in status, got %q", st.LastNotes)
	}

	// HANDOFF.md mirror generated with all key info.
	data, err := os.ReadFile(filepath.Join(dir, MirrorFileName))
	if err != nil {
		t.Fatalf("expected HANDOFF.md to exist: %v", err)
	}
	for _, want := range []string{"antigravity-ide", "gen diagram", "โฟกัสที่ pipeline หลักพอ", "src/main.go"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("HANDOFF.md missing %q:\n%s", want, data)
		}
	}

	// Agents roster: sender was touched by the handoff, receiver never seen.
	infos, err := o.Agents()
	if err != nil {
		t.Fatalf("Agents failed: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(infos))
	}
	byName := map[string]AgentInfo{}
	for _, a := range infos {
		byName[a.Name] = a
	}
	if !byName["claude-code"].Online {
		t.Error("expected claude-code to be online after sending a handoff")
	}
	if byName["antigravity-ide"].Online {
		t.Error("expected antigravity-ide to be offline (never active)")
	}
	if byName["claude-code"].Description != "coder" {
		t.Errorf("expected capability profile to round-trip, got %+v", byName["claude-code"])
	}
}

func TestHandoffRejectsUnknownTargetAgent(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	if err := o.Init("proj", workspace.AgentsFromNames([]string{"a", "b"}), nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	_, err := o.Handoff(HandoffRequest{SourceAgent: "a", TargetAgent: "ghost", Task: "x"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected rejection naming the unknown agent, got: %v", err)
	}
}

func TestHandoffRejectsUnknownStage(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	if err := o.Init("proj", workspace.AgentsFromNames([]string{"a", "b"}), []string{"planning", "coding"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	_, err := o.Handoff(HandoffRequest{SourceAgent: "a", TargetAgent: "b", Stage: "deploying", Task: "x"})
	if err == nil {
		t.Fatal("expected error for stage outside configured set")
	}
}

func TestStatusOnUninitializedDirStaysUninitialized(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)

	// Simulate the debug logger having created .ao/logs/ (e.g. from a
	// prior failed call) without the workspace ever being `ao init`-ed.
	// A bare .ao/ directory must never be mistaken for initialization.
	if err := os.MkdirAll(filepath.Join(dir, ".ao", "logs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := o.Status(); err == nil {
		t.Fatal("expected error for uninitialized workspace despite .ao/ existing")
	}
	if _, err := o.Handoff(HandoffRequest{SourceAgent: "a", TargetAgent: "b", Task: "x"}); err == nil {
		t.Fatal("expected Handoff to also reject the uninitialized workspace")
	}
}

func TestHandoffRequiresInit(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	_, err := o.Handoff(HandoffRequest{SourceAgent: "a", TargetAgent: "b", Task: "x"})
	if err == nil {
		t.Fatal("expected error when workspace is not initialized")
	}
}
