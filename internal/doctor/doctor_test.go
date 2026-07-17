package doctor

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
	"github.com/ponthepmk/AgentOrchestra/internal/registry"
	"github.com/ponthepmk/AgentOrchestra/internal/scaffold"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

func byName(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func setupHealthyProject(t *testing.T) string {
	t.Helper()
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	o := orchestrator.New(dir)
	team := []workspace.Agent{{Name: "claude-code"}, {Name: "antigravity-ide"}}
	if err := o.Init("proj", team, []string{"planning", "coding"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := scaffold.Apply(dir, team); err != nil {
		t.Fatalf("scaffold failed: %v", err)
	}
	if err := registry.Register("proj", dir); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	return dir
}

func TestHealthyProjectPassesProjectChecks(t *testing.T) {
	dir := setupHealthyProject(t)
	checks := Run(dir, "")

	for _, name := range []string{"state (.ao/state.json)", "team (.agentconfig)", "MCP config (.mcp.json)", "instructions (CLAUDE.md)", "index (.ao/index.db)", "project registry"} {
		c, found := byName(checks, name)
		if !found {
			t.Errorf("missing check %q", name)
			continue
		}
		if !c.OK {
			t.Errorf("expected %q to pass, got: %s", name, c.Detail)
		}
	}
}

func TestUninitializedWorkspaceFails(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	checks := Run(t.TempDir(), "")
	c, found := byName(checks, "workspace")
	if !found || c.OK {
		t.Fatalf("expected workspace check to fail for uninitialized dir, got %+v", c)
	}
	if AllOK(checks) {
		t.Fatal("expected AllOK to be false")
	}
}

func TestBrokenMCPJSONDetected(t *testing.T) {
	dir := setupHealthyProject(t)
	if err := os.Remove(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	checks := Run(dir, "")
	c, _ := byName(checks, "MCP config (.mcp.json)")
	if c.OK {
		t.Fatal("expected missing .mcp.json to be detected")
	}
	if c.Fix == "" {
		t.Fatal("expected a fix hint")
	}
}

func TestIndexMismatchDetected(t *testing.T) {
	dir := setupHealthyProject(t)
	o := orchestrator.New(dir)
	if _, err := o.Handoff(orchestrator.HandoffRequest{
		SourceAgent: "claude-code", TargetAgent: "antigravity-ide", Task: "x",
	}); err != nil {
		t.Fatal(err)
	}
	// Corrupt the invariant: delete the index so counts can't match.
	if err := os.Remove(filepath.Join(dir, ".ao", "index.db")); err != nil {
		t.Fatal(err)
	}
	checks := Run(dir, "")
	c, _ := byName(checks, "index (.ao/index.db)")
	if c.OK {
		t.Fatalf("expected index mismatch to be detected, got: %s", c.Detail)
	}
}

func TestWorkerProbe(t *testing.T) {
	dir := setupHealthyProject(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checks := Run(dir, srv.URL+"/v1")
	c, found := byName(checks, "worker model server")
	if !found || !c.OK {
		t.Fatalf("expected reachable worker probe to pass, got %+v", c)
	}

	srv.Close()
	checks = Run(dir, srv.URL+"/v1")
	c, _ = byName(checks, "worker model server")
	if c.OK {
		t.Fatal("expected probe against closed server to fail")
	}
}
