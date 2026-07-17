package presence

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestTouchAndGet(t *testing.T) {
	dir := t.TempDir()
	if err := Touch(dir, "claude-code", "status"); err != nil {
		t.Fatalf("Touch failed: %v", err)
	}
	rec := Get(dir, "claude-code")
	if rec.Agent != "claude-code" || rec.Via != "status" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if !rec.Online() {
		t.Fatal("expected freshly touched agent to be online")
	}
}

func TestGetUnknownAgentIsOffline(t *testing.T) {
	dir := t.TempDir()
	rec := Get(dir, "never-seen")
	if rec.Online() {
		t.Fatal("expected unknown agent to be offline")
	}
}

func TestStaleRecordIsOffline(t *testing.T) {
	dir := t.TempDir()
	if err := Touch(dir, "old-agent", "handoff"); err != nil {
		t.Fatalf("Touch failed: %v", err)
	}
	// Rewrite the record with a stale timestamp.
	rec := Record{Agent: "old-agent", LastSeen: time.Now().Add(-OnlineWindow - time.Minute), Via: "handoff"}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(fileFor(dir, "old-agent"), data, 0o644); err != nil {
		t.Fatalf("rewrite record: %v", err)
	}
	if Get(dir, "old-agent").Online() {
		t.Fatal("expected stale agent to be offline")
	}
}

func TestTouchEmptyAgentIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := Touch(dir, "", "status"); err != nil {
		t.Fatalf("Touch with empty agent should be a no-op, got: %v", err)
	}
	if _, err := os.Stat(presenceDir(dir)); !os.IsNotExist(err) {
		t.Fatal("expected no presence dir to be created for empty agent")
	}
}
