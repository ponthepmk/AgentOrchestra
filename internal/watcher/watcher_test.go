package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestWatcherTriggersHandoffOnMatchingFile(t *testing.T) {
	dir := t.TempDir()
	orc := orchestrator.New(dir)
	if err := orc.Init("proj", []string{"claude-code", "antigravity-ide"}, []string{"planning", "coding", "documenting"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	w, err := New(Config{
		Dir:         dir,
		SourceAgent: "claude-code",
		TargetAgent: "antigravity-ide",
		Stage:       "documenting",
		Patterns:    []string{"*.md"},
		Debounce:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, discardLogger()) }()

	specPath := filepath.Join(dir, "SPEC.md")
	if err := os.WriteFile(specPath, []byte("# spec v1"), 0o644); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var records []struct{ found bool }
	for {
		select {
		case <-tick.C:
			recs, err := orc.Log(orchestrator.LogFilter{})
			if err == nil && len(recs) == 1 {
				if recs[0].TargetAgent != "antigravity-ide" || recs[0].Stage != "documenting" {
					t.Fatalf("unexpected handoff record: %+v", recs[0])
				}
				cancel()
				<-done
				return
			}
			_ = records
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timed out waiting for auto-handoff after file write")
		}
	}
}

func TestWatcherIgnoresNonMatchingFile(t *testing.T) {
	dir := t.TempDir()
	orc := orchestrator.New(dir)
	if err := orc.Init("proj", []string{"claude-code", "antigravity-ide"}, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	w, err := New(Config{
		Dir: dir, SourceAgent: "claude-code", TargetAgent: "antigravity-ide",
		Patterns: []string{"*.md"}, Debounce: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, discardLogger()) }()

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("irrelevant"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done

	recs, err := orc.Log(orchestrator.LogFilter{})
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected no auto-handoff for a non-matching file, got %+v", recs)
	}
}
