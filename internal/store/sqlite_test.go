package store

import (
	"testing"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/model"
)

func entry(source, target, stage, task string, ts time.Time) Entry {
	return Entry{
		PayloadFile: "handoffs/x.json",
		Envelope: model.Envelope{
			ProjectID:    "proj",
			CurrentStage: stage,
			SourceAgent:  source,
			TargetAgent:  target,
			Payload:      model.Payload{Task: task},
			Metadata:     model.Metadata{Timestamp: ts, Version: "1.0.0"},
		},
	}
}

func TestSaveAndListHandoffs(t *testing.T) {
	s, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer s.Close()

	base := time.Now().UTC()
	if err := s.SaveHandoff(entry("claude-code", "antigravity-ide", "coding", "gen diagram", base)); err != nil {
		t.Fatalf("SaveHandoff 1 failed: %v", err)
	}
	if err := s.SaveHandoff(entry("antigravity-ide", "claude-code", "documenting", "review diagram", base.Add(time.Second))); err != nil {
		t.Fatalf("SaveHandoff 2 failed: %v", err)
	}

	records, err := s.ListHandoffs("proj", 0)
	if err != nil {
		t.Fatalf("ListHandoffs failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Task != "review diagram" {
		t.Fatalf("expected most-recent-first order, got %q first", records[0].Task)
	}

	proj, err := s.ProjectState("proj")
	if err != nil {
		t.Fatalf("ProjectState failed: %v", err)
	}
	if proj.CurrentStage != "documenting" || proj.HolderAgent != "claude-code" {
		t.Fatalf("unexpected project state: %+v", proj)
	}
}

func TestReindex(t *testing.T) {
	s, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer s.Close()

	base := time.Now().UTC()
	if err := s.SaveHandoff(entry("a", "b", "planning", "old task", base)); err != nil {
		t.Fatalf("SaveHandoff failed: %v", err)
	}

	entries := []Entry{
		entry("a", "b", "coding", "task 1", base.Add(time.Second)),
		entry("b", "a", "documenting", "task 2", base.Add(2*time.Second)),
	}
	if err := s.Reindex("proj", entries); err != nil {
		t.Fatalf("Reindex failed: %v", err)
	}

	records, err := s.ListHandoffs("proj", 0)
	if err != nil {
		t.Fatalf("ListHandoffs failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected reindex to replace history with exactly 2 records, got %d", len(records))
	}

	proj, err := s.ProjectState("proj")
	if err != nil {
		t.Fatalf("ProjectState failed: %v", err)
	}
	if proj.CurrentStage != "documenting" || proj.HolderAgent != "a" {
		t.Fatalf("unexpected project state after reindex: %+v", proj)
	}
}

func TestProjectStateNotFound(t *testing.T) {
	s, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer s.Close()

	if _, err := s.ProjectState("missing"); err == nil {
		t.Fatal("expected error for unknown project")
	}
}
