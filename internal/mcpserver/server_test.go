package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect starts the AgentOrchestra MCP server on an in-memory transport and
// returns a connected client session for calling its tools.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := New("test")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callText(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) failed: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned tool error: %v", tool, res.Content)
	}
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s) expected TextContent, got %T", tool, res.Content[0])
	}
	return tc.Text
}

func TestListTools(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"ao_init", "ao_status", "ao_handoff", "ao_log", "ao_agents", "ao_projects", "ao_remember", "ao_recall", "ao_stats"} {
		if !names[want] {
			t.Errorf("expected tool %q to be registered, got %v", want, names)
		}
	}
}

func TestRememberRecallStatsRoundTrip(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	dir := t.TempDir()
	callText(t, session, "ao_init", map[string]any{"project_dir": dir, "project_id": "proj"})

	callText(t, session, "ao_remember", map[string]any{
		"project_dir": dir, "agent": "claude-code",
		"key": "db-choice", "value": "ใช้ SQLite เพราะ home lab", "tags": []string{"decision"},
	})

	recallMsg := callText(t, session, "ao_recall", map[string]any{"project_dir": dir, "key": "db-choice"})
	if !strings.Contains(recallMsg, "SQLite") || !strings.Contains(recallMsg, "claude-code") {
		t.Fatalf("expected recall to include value and author, got: %s", recallMsg)
	}

	callText(t, session, "ao_handoff", map[string]any{
		"project_dir": dir, "source_agent": "claude-code", "target_agent": "antigravity-ide", "task": "t1",
	})
	statsMsg := callText(t, session, "ao_stats", map[string]any{"project_dir": dir})
	if !strings.Contains(statsMsg, "total handoffs: 1") {
		t.Fatalf("expected stats to count the handoff, got: %s", statsMsg)
	}
}

func TestAgentsAndDefaultProjectResolution(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	dir := t.TempDir()

	// Init with the team preset (no agents arg) — registers dir as default.
	callText(t, session, "ao_init", map[string]any{"project_dir": dir, "project_id": "proj"})

	// ao_agents with NO project reference must resolve via the default, and
	// passing our own name must mark us online.
	agentsMsg := callText(t, session, "ao_agents", map[string]any{"agent": "claude-code"})
	for _, want := range []string{"claude-code", "codex", "antigravity-ide", "ollama-worker", "infographic"} {
		if !strings.Contains(agentsMsg, want) {
			t.Errorf("expected ao_agents to mention %q, got: %s", want, agentsMsg)
		}
	}
	if !strings.Contains(agentsMsg, "claude-code [ONLINE]") {
		t.Errorf("expected claude-code to be online after identifying itself, got: %s", agentsMsg)
	}

	// ao_status without project_dir works via the default project too.
	statusMsg := callText(t, session, "ao_status", map[string]any{})
	if !strings.Contains(statusMsg, "project: proj") {
		t.Fatalf("expected default-project status, got: %s", statusMsg)
	}

	// ao_projects lists the registered project as default.
	projMsg := callText(t, session, "ao_projects", map[string]any{})
	if !strings.Contains(projMsg, "proj (default)") {
		t.Fatalf("expected ao_projects to list default project, got: %s", projMsg)
	}

	// Handing off to an agent outside the roster is rejected.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ao_handoff",
		Arguments: map[string]any{
			"source_agent": "claude-code", "target_agent": "ghost", "task": "x",
		},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for target agent not on the team")
	}
}

func TestInitStatusHandoffLogRoundTrip(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	dir := t.TempDir()

	initMsg := callText(t, session, "ao_init", map[string]any{
		"project_dir": dir,
		"project_id":  "ai-trading-hub",
		"agents":      []string{"claude-code", "antigravity-ide"},
		"stages":      []string{"planning", "coding", "documenting"},
	})
	if !strings.Contains(initMsg, "ai-trading-hub") {
		t.Fatalf("unexpected ao_init result: %s", initMsg)
	}

	statusMsg := callText(t, session, "ao_status", map[string]any{"project_dir": dir})
	if !strings.Contains(statusMsg, "stage: planning") {
		t.Fatalf("expected initial stage planning, got: %s", statusMsg)
	}

	handoffMsg := callText(t, session, "ao_handoff", map[string]any{
		"project_dir":  dir,
		"source_agent": "claude-code",
		"target_agent": "antigravity-ide",
		"stage":        "documenting",
		"task":         "generate architecture diagram",
		"artifacts":    []string{"src/train.py"},
	})
	if !strings.Contains(handoffMsg, "claude-code") || !strings.Contains(handoffMsg, "antigravity-ide") {
		t.Fatalf("unexpected ao_handoff result: %s", handoffMsg)
	}

	statusMsg = callText(t, session, "ao_status", map[string]any{"project_dir": dir})
	if !strings.Contains(statusMsg, "stage: documenting") || !strings.Contains(statusMsg, "holder_agent: antigravity-ide") {
		t.Fatalf("expected updated status, got: %s", statusMsg)
	}

	logMsg := callText(t, session, "ao_log", map[string]any{"project_dir": dir})
	if !strings.Contains(logMsg, "generate architecture diagram") {
		t.Fatalf("expected log to contain handoff task, got: %s", logMsg)
	}
}

func TestLogFilterByStageAndAgent(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	dir := t.TempDir()
	callText(t, session, "ao_init", map[string]any{
		"project_dir": dir, "project_id": "proj",
		"agents": []string{"claude-code", "antigravity-ide", "codex"},
		"stages": []string{"planning", "coding", "documenting"},
	})
	callText(t, session, "ao_handoff", map[string]any{
		"project_dir": dir, "source_agent": "claude-code", "target_agent": "antigravity-ide",
		"stage": "coding", "task": "task 1",
	})
	callText(t, session, "ao_handoff", map[string]any{
		"project_dir": dir, "source_agent": "antigravity-ide", "target_agent": "codex",
		"stage": "documenting", "task": "task 2",
	})

	byStage := callText(t, session, "ao_log", map[string]any{"project_dir": dir, "stage": "coding"})
	if !strings.Contains(byStage, "task 1") || strings.Contains(byStage, "task 2") {
		t.Fatalf("expected stage filter to return only task 1, got: %s", byStage)
	}

	byAgent := callText(t, session, "ao_log", map[string]any{"project_dir": dir, "agent": "codex"})
	if !strings.Contains(byAgent, "task 2") || strings.Contains(byAgent, "task 1") {
		t.Fatalf("expected agent filter to return only task 2, got: %s", byAgent)
	}
}

func TestStatusBeforeInitReturnsToolError(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	dir := t.TempDir()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ao_status",
		Arguments: map[string]any{"project_dir": dir},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool-level error when workspace is not initialized")
	}
}

func TestHandoffStructuredOutput(t *testing.T) {
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	session := connect(t)
	dir := t.TempDir()
	callText(t, session, "ao_init", map[string]any{"project_dir": dir, "project_id": "proj"})

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ao_handoff",
		Arguments: map[string]any{
			"project_dir":  dir,
			"source_agent": "claude-code",
			"target_agent": "antigravity-ide",
			"task":         "do the thing",
		},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if !strings.Contains(string(raw), "do the thing") {
		t.Fatalf("expected structured content to include task, got: %s", raw)
	}
}
