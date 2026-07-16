package orchestrator

import "testing"

func TestFullHandoffCycle(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)

	if err := o.Init("ai-trading-hub", []string{"claude-code", "antigravity-ide"}, []string{"planning", "coding", "documenting"}); err != nil {
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

	records, err := o.Log(0)
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
	records, err = o.Log(0)
	if err != nil {
		t.Fatalf("Log after reindex failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after reindex, got %d", len(records))
	}
}

func TestHandoffRejectsUnknownStage(t *testing.T) {
	dir := t.TempDir()
	o := New(dir)
	if err := o.Init("proj", []string{"a", "b"}, []string{"planning", "coding"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	_, err := o.Handoff(HandoffRequest{SourceAgent: "a", TargetAgent: "b", Stage: "deploying", Task: "x"})
	if err == nil {
		t.Fatal("expected error for stage outside configured set")
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
