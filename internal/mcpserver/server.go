// Package mcpserver exposes AgentOrchestra's orchestrator as MCP tools so
// any MCP-capable agent (Claude Code, Antigravity, Codex, ...) can call
// ao_init/ao_status/ao_handoff/ao_log directly instead of a human copying
// context between them by hand.
package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
)

// New builds an MCP server with all AgentOrchestra tools registered. Every
// tool call is traced to slog's default logger (stderr by default) with
// its name, duration, and outcome, in addition to the per-project debug
// log each orchestrator operation writes to .ao/logs/ao.log.
func New(version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agentorchestra",
		Version: version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_init",
		Description: "Initialize AgentOrchestra (.ao/ and .agentconfig) for a project directory. Idempotent — safe to call even if already initialized.",
	}, logged("ao_init", handleInit))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_status",
		Description: "Get the current stage, holder agent, and last task for a project — call this first to see whose turn it is before doing work.",
	}, logged("ao_status", handleStatus))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_handoff",
		Description: "Hand off context to another agent: records what was done, what's requested next, and which artifacts are relevant. Call this when your part of the work is done.",
	}, logged("ao_handoff", handleHandoff))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_log",
		Description: "List the handoff history for a project, most recent first. Optionally filter by stage and/or agent.",
	}, logged("ao_log", handleLog))

	return server
}

// logged wraps a tool handler so every call is traced via slog — useful for
// debugging what an agent actually called and when, independent of each
// project's own .ao/logs/ao.log.
func logged[In any](toolName string, h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error)) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		res, out, err := h(ctx, req, args)
		dur := time.Since(start)
		switch {
		case err != nil:
			slog.Error("mcp tool call failed", "tool", toolName, "duration_ms", dur.Milliseconds(), "error", err)
		case res != nil && res.IsError:
			slog.Warn("mcp tool call returned error result", "tool", toolName, "duration_ms", dur.Milliseconds())
		default:
			slog.Info("mcp tool call ok", "tool", toolName, "duration_ms", dur.Milliseconds())
		}
		return res, out, err
	}
}

type initArgs struct {
	ProjectDir string   `json:"project_dir" jsonschema:"absolute path to the project's root directory"`
	ProjectID  string   `json:"project_id" jsonschema:"unique identifier for the project, e.g. ai-trading-hub"`
	Agents     []string `json:"agents,omitempty" jsonschema:"names of agents that will plug into this project, e.g. [claude-code, antigravity-ide]"`
	Stages     []string `json:"stages,omitempty" jsonschema:"ordered list of workflow stages; defaults to [planning, coding, documenting] if omitted"`
}

func handleInit(ctx context.Context, req *mcp.CallToolRequest, args initArgs) (*mcp.CallToolResult, any, error) {
	o := orchestrator.New(args.ProjectDir)
	if err := o.Init(args.ProjectID, args.Agents, args.Stages); err != nil {
		return errResult(err), nil, nil
	}
	return textResult(fmt.Sprintf("initialized AgentOrchestra workspace for project %q in %s", args.ProjectID, args.ProjectDir)), nil, nil
}

type statusArgs struct {
	ProjectDir string `json:"project_dir" jsonschema:"absolute path to the project's root directory"`
}

func handleStatus(ctx context.Context, req *mcp.CallToolRequest, args statusArgs) (*mcp.CallToolResult, any, error) {
	o := orchestrator.New(args.ProjectDir)
	st, err := o.Status()
	if err != nil {
		return errResult(err), nil, nil
	}
	msg := fmt.Sprintf(
		"project: %s\nstage: %s\nholder_agent: %s\nlast_task: %s\nupdated_at: %s\nstages: %v\nagents: %v",
		st.ProjectID, st.CurrentStage, st.HolderAgent, st.LastTask, st.UpdatedAt.Format("2006-01-02T15:04:05Z"), st.Stages, st.Agents,
	)
	return textResult(msg), st, nil
}

type handoffArgs struct {
	ProjectDir  string         `json:"project_dir" jsonschema:"absolute path to the project's root directory"`
	SourceAgent string         `json:"source_agent" jsonschema:"the agent handing off (you)"`
	TargetAgent string         `json:"target_agent" jsonschema:"the agent to hand off to"`
	Stage       string         `json:"stage,omitempty" jsonschema:"the stage to move the project to; defaults to the current stage if omitted"`
	Task        string         `json:"task" jsonschema:"what the target agent should do next"`
	Artifacts   []string       `json:"artifacts,omitempty" jsonschema:"file paths relevant to this handoff"`
	Extra       map[string]any `json:"extra,omitempty" jsonschema:"any additional structured context"`
}

func handleHandoff(ctx context.Context, req *mcp.CallToolRequest, args handoffArgs) (*mcp.CallToolResult, any, error) {
	o := orchestrator.New(args.ProjectDir)
	env, err := o.Handoff(orchestrator.HandoffRequest{
		SourceAgent: args.SourceAgent,
		TargetAgent: args.TargetAgent,
		Stage:       args.Stage,
		Task:        args.Task,
		Artifacts:   args.Artifacts,
		Extra:       args.Extra,
	})
	if err != nil {
		return errResult(err), nil, nil
	}
	msg := fmt.Sprintf("handed off %q -> %q for project %q (stage: %s): %s",
		env.SourceAgent, env.TargetAgent, env.ProjectID, env.CurrentStage, env.Payload.Task)
	return textResult(msg), env, nil
}

type logArgs struct {
	ProjectDir string `json:"project_dir" jsonschema:"absolute path to the project's root directory"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max number of handoffs to return; 0 means all"`
	Stage      string `json:"stage,omitempty" jsonschema:"only return handoffs at this stage"`
	Agent      string `json:"agent,omitempty" jsonschema:"only return handoffs where this agent is the source or the target"`
}

func handleLog(ctx context.Context, req *mcp.CallToolRequest, args logArgs) (*mcp.CallToolResult, any, error) {
	o := orchestrator.New(args.ProjectDir)
	records, err := o.Log(orchestrator.LogFilter{Stage: args.Stage, Agent: args.Agent, Limit: args.Limit})
	if err != nil {
		return errResult(err), nil, nil
	}
	if len(records) == 0 {
		return textResult("no handoffs recorded yet"), records, nil
	}
	msg := ""
	for _, r := range records {
		msg += fmt.Sprintf("[%s] %s -> %s (%s): %s\n", r.CreatedAt.Format("2006-01-02T15:04:05Z"), r.SourceAgent, r.TargetAgent, r.Stage, r.Task)
	}
	return textResult(msg), records, nil
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
