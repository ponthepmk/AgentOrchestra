package ui

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

func TestSummaryEndpoint(t *testing.T) {
	dir := t.TempDir()
	o := orchestrator.New(dir)
	if err := o.Init("proj", workspace.AgentsFromNames([]string{"claude-code", "antigravity-ide"}), nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if _, err := o.Handoff(orchestrator.HandoffRequest{
		SourceAgent: "claude-code", TargetAgent: "antigravity-ide",
		Task: "เจน diagram", Notes: "โฟกัส pipeline",
	}); err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}
	if _, err := o.Remember("claude-code", "db-choice", "SQLite", nil); err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dir))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/summary")
	if err != nil {
		t.Fatalf("GET /api/summary failed: %v", err)
	}
	defer resp.Body.Close()

	var s summary
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if s.Error != "" {
		t.Fatalf("unexpected error: %s", s.Error)
	}
	if s.Status == nil || s.Status.HolderAgent != "antigravity-ide" {
		t.Fatalf("unexpected status: %+v", s.Status)
	}
	if len(s.Agents) != 2 || len(s.Handoffs) != 1 || len(s.Memories) != 1 {
		t.Fatalf("unexpected counts: agents=%d handoffs=%d memories=%d", len(s.Agents), len(s.Handoffs), len(s.Memories))
	}
	if s.Stats == nil || s.Stats.TotalHandoffs != 1 {
		t.Fatalf("unexpected stats: %+v", s.Stats)
	}
}

func TestSummaryUninitializedReportsError(t *testing.T) {
	srv := httptest.NewServer(NewHandler(t.TempDir()))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/summary")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	var s summary
	json.NewDecoder(resp.Body).Decode(&s)
	if s.Error == "" {
		t.Fatal("expected error for uninitialized workspace")
	}
}

func TestIndexPageServed(t *testing.T) {
	srv := httptest.NewServer(NewHandler(t.TempDir()))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "AgentOrchestra") || !strings.Contains(body, "/api/summary") {
		t.Fatalf("unexpected index page (first 200 bytes): %s", body[:200])
	}
}
