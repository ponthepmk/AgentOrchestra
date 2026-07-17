package orchestrator

import (
	"testing"

	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

func TestStats(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	if err := o.Init("proj", workspace.AgentsFromNames([]string{"a", "b", "c"}), []string{"planning", "coding"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	must := func(from, to, stage, task string) {
		t.Helper()
		if _, err := o.Handoff(HandoffRequest{SourceAgent: from, TargetAgent: to, Stage: stage, Task: task}); err != nil {
			t.Fatalf("Handoff %s->%s failed: %v", from, to, err)
		}
	}
	must("a", "b", "planning", "t1")
	must("b", "a", "planning", "t2") // quick return (b bounced back to a immediately)
	must("a", "c", "coding", "t3")

	stats, err := o.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalHandoffs != 3 {
		t.Fatalf("expected 3 handoffs, got %d", stats.TotalHandoffs)
	}
	if stats.QuickReturns != 1 {
		t.Fatalf("expected 1 quick return, got %d", stats.QuickReturns)
	}
	if stats.PerStage["planning"] != 2 || stats.PerStage["coding"] != 1 {
		t.Fatalf("unexpected per-stage counts: %v", stats.PerStage)
	}

	byAgent := map[string]AgentStats{}
	for _, a := range stats.PerAgent {
		byAgent[a.Agent] = a
	}
	if byAgent["a"].Sent != 2 || byAgent["a"].Received != 1 {
		t.Fatalf("unexpected stats for a: %+v", byAgent["a"])
	}
	if byAgent["b"].Sent != 1 || byAgent["b"].Received != 1 {
		t.Fatalf("unexpected stats for b: %+v", byAgent["b"])
	}
	if byAgent["c"].Received != 1 {
		t.Fatalf("unexpected stats for c: %+v", byAgent["c"])
	}
}

func TestStatsEmptyProject(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	if err := o.Init("proj", workspace.AgentsFromNames([]string{"a", "b"}), nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	stats, err := o.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalHandoffs != 0 || stats.QuickReturns != 0 {
		t.Fatalf("expected zero activity, got %+v", stats)
	}
}

func TestRememberRecallForget(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	if err := o.Init("proj", workspace.AgentsFromNames([]string{"a", "b"}), nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if _, err := o.Remember("a", "db-choice", "SQLite", []string{"decision"}); err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	entries, err := o.Recall("b", "db-choice", "", "")
	if err != nil || len(entries) != 1 || entries[0].Value != "SQLite" {
		t.Fatalf("Recall by key failed: %v / %+v", err, entries)
	}

	entries, err = o.Recall("", "", "decision", "")
	if err != nil || len(entries) != 1 {
		t.Fatalf("Recall by tag failed: %v / %+v", err, entries)
	}

	if err := o.Forget("db-choice"); err != nil {
		t.Fatalf("Forget failed: %v", err)
	}
	if _, err := o.Recall("", "db-choice", "", ""); err == nil {
		t.Fatal("expected Recall to fail after Forget")
	}
}
