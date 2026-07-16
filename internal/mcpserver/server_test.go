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
	session := connect(t)
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"ao_init", "ao_status", "ao_handoff", "ao_log"} {
		if !names[want] {
			t.Errorf("expected tool %q to be registered, got %v", want, names)
		}
	}
}

func TestInitStatusHandoffLogRoundTrip(t *testing.T) {
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

func TestStatusBeforeInitReturnsToolError(t *testing.T) {
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
