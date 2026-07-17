// Package mcpserver exposes AgentOrchestra's orchestrator as MCP tools so
// any MCP-capable agent (Claude Code, Claude Desktop, Antigravity, Codex,
// OpenCode, ...) can call ao_* tools directly instead of a human copying
// context between them by hand.
package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
	"github.com/ponthepmk/AgentOrchestra/internal/registry"
	"github.com/ponthepmk/AgentOrchestra/internal/scaffold"
	"github.com/ponthepmk/AgentOrchestra/internal/state"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
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
		Description: "Initialize AgentOrchestra for a project directory and register it so later calls can address it by project_id alone. Idempotent — safe to call even if already initialized.",
	}, logged("ao_init", handleInit))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_agents",
		Description: "List this project's team: every agent's name, what it's good at (capabilities), and whether it's online right now. Call this when you connect, before picking a target for a handoff.",
	}, logged("ao_agents", handleAgents))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_status",
		Description: "Get the current stage, holder agent, and last task/notes for a project — call this to see whose turn it is before doing work.",
	}, logged("ao_status", handleStatus))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_handoff",
		Description: "Hand off context to another agent: records what was done, what's requested next (task + notes), and which artifacts are relevant. Call this when your part of the work is done. Pick target_agent based on the team's capabilities (see ao_agents).",
	}, logged("ao_handoff", handleHandoff))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_log",
		Description: "List the handoff history for a project, most recent first. Optionally filter by stage and/or agent.",
	}, logged("ao_log", handleLog))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_projects",
		Description: "List all projects registered on this machine (id and directory), and which one is the default when a call names no project.",
	}, logged("ao_projects", handleProjects))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_remember",
		Description: "Save a shared team memory (decision, gotcha, fact) that OTHER agents can recall later — unlike ao_handoff this does NOT pass the baton. Same key overwrites. Never store secrets here.",
	}, logged("ao_remember", handleRemember))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_recall",
		Description: "Read shared team memories: one entry by key, or list by tag/substring. Check updated_at/author — entries can be stale.",
	}, logged("ao_recall", handleRecall))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ao_stats",
		Description: "Handoff activity numbers for a project: totals, per-agent sent/received and average hold time, per-stage counts, quick returns.",
	}, logged("ao_stats", handleStats))

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

// projectRef is embedded in every tool's args: name the project by id, by
// directory, or not at all (default project).
type projectRef struct {
	ProjectID  string `json:"project_id,omitempty" jsonschema:"registered project id (see ao_projects); omit to use the default project"`
	ProjectDir string `json:"project_dir,omitempty" jsonschema:"absolute path to the project root; overrides project_id when both are given"`
}

func (p projectRef) resolve() (*orchestrator.Orchestrator, error) {
	dir, err := registry.Resolve(p.ProjectID, p.ProjectDir)
	if err != nil {
		return nil, err
	}
	return orchestrator.New(dir), nil
}

type initArgs struct {
	ProjectDir string   `json:"project_dir" jsonschema:"absolute path to the project's root directory"`
	ProjectID  string   `json:"project_id" jsonschema:"unique identifier for the project, e.g. ai-trading-hub"`
	Agents     []string `json:"agents,omitempty" jsonschema:"agent names to seed the team with; omit to get the standard team preset (claude-code, codex, antigravity-ide, ollama-worker)"`
	Stages     []string `json:"stages,omitempty" jsonschema:"ordered list of workflow stages; defaults to [planning, coding, documenting] if omitted"`
}

func handleInit(ctx context.Context, req *mcp.CallToolRequest, args initArgs) (*mcp.CallToolResult, any, error) {
	var agents []workspace.Agent
	if len(args.Agents) > 0 {
		agents = workspace.AgentsFromNames(args.Agents)
	} else {
		agents = scaffold.TeamPreset()
	}
	stages := args.Stages
	if len(stages) == 0 {
		stages = state.DefaultStages
	}

	o := orchestrator.New(args.ProjectDir)
	if err := o.Init(args.ProjectID, agents, stages); err != nil {
		return errResult(err), nil, nil
	}
	if err := scaffold.Apply(args.ProjectDir, agents); err != nil {
		return errResult(fmt.Errorf("scaffold: %w", err)), nil, nil
	}
	if err := registry.Register(args.ProjectID, args.ProjectDir); err != nil {
		return errResult(fmt.Errorf("register project: %w", err)), nil, nil
	}
	return textResult(fmt.Sprintf("initialized AgentOrchestra workspace for project %q in %s (registered; scaffolded .mcp.json, CLAUDE.md, AGENTS.md)", args.ProjectID, args.ProjectDir)), nil, nil
}

type agentsArgs struct {
	projectRef
	Agent string `json:"agent,omitempty" jsonschema:"your own agent name — pass it so teammates can see you online"`
}

func handleAgents(ctx context.Context, req *mcp.CallToolRequest, args agentsArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
	o.TouchPresence(args.Agent, "agents")
	infos, err := o.Agents()
	if err != nil {
		return errResult(err), nil, nil
	}
	if len(infos) == 0 {
		return textResult("no agents configured — add them to .agentconfig"), infos, nil
	}
	msg := ""
	for _, a := range infos {
		status := "offline"
		if a.Online {
			status = "ONLINE"
		} else if !a.LastSeen.IsZero() {
			status = "last seen " + a.LastSeen.Format("2006-01-02 15:04") + " UTC"
		}
		msg += fmt.Sprintf("- %s [%s]", a.Name, status)
		if len(a.Capabilities) > 0 {
			msg += fmt.Sprintf(" capabilities: %v", a.Capabilities)
		}
		if a.Description != "" {
			msg += " — " + a.Description
		}
		msg += "\n"
	}
	return textResult(msg), infos, nil
}

type statusArgs struct {
	projectRef
	Agent string `json:"agent,omitempty" jsonschema:"your own agent name — pass it so teammates can see you online"`
}

func handleStatus(ctx context.Context, req *mcp.CallToolRequest, args statusArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
	o.TouchPresence(args.Agent, "status")
	st, err := o.Status()
	if err != nil {
		return errResult(err), nil, nil
	}
	msg := fmt.Sprintf(
		"project: %s\nstage: %s\nholder_agent: %s\nlast_task: %s\nupdated_at: %s\nstages: %v\nagents: %v",
		st.ProjectID, st.CurrentStage, st.HolderAgent, st.LastTask, st.UpdatedAt.Format("2006-01-02T15:04:05Z"), st.Stages, st.Agents,
	)
	if st.LastNotes != "" {
		msg += "\nnotes:\n" + st.LastNotes
	}
	return textResult(msg), st, nil
}

type handoffArgs struct {
	projectRef
	SourceAgent string         `json:"source_agent" jsonschema:"the agent handing off (you)"`
	TargetAgent string         `json:"target_agent" jsonschema:"the agent to hand off to — must be on the project's team (see ao_agents)"`
	Stage       string         `json:"stage,omitempty" jsonschema:"the stage to move the project to; defaults to the current stage if omitted"`
	Task        string         `json:"task" jsonschema:"what the target agent should do next (short imperative)"`
	Notes       string         `json:"notes,omitempty" jsonschema:"longer free-form context for the target agent (markdown ok): decisions made, gotchas, where to look"`
	Artifacts   []string       `json:"artifacts,omitempty" jsonschema:"file paths relevant to this handoff"`
	Extra       map[string]any `json:"extra,omitempty" jsonschema:"any additional structured context"`
}

func handleHandoff(ctx context.Context, req *mcp.CallToolRequest, args handoffArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
	env, err := o.Handoff(orchestrator.HandoffRequest{
		SourceAgent: args.SourceAgent,
		TargetAgent: args.TargetAgent,
		Stage:       args.Stage,
		Task:        args.Task,
		Notes:       args.Notes,
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
	projectRef
	Limit int    `json:"limit,omitempty" jsonschema:"max number of handoffs to return; 0 means all"`
	Stage string `json:"stage,omitempty" jsonschema:"only return handoffs at this stage"`
	Agent string `json:"agent,omitempty" jsonschema:"only return handoffs where this agent is the source or the target"`
}

func handleLog(ctx context.Context, req *mcp.CallToolRequest, args logArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
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

type projectsArgs struct{}

func handleProjects(ctx context.Context, req *mcp.CallToolRequest, args projectsArgs) (*mcp.CallToolResult, any, error) {
	f, err := registry.Load()
	if err != nil {
		return errResult(err), nil, nil
	}
	if len(f.Projects) == 0 {
		return textResult("no projects registered yet — run ao_init (or `ao init`) in a project first"), f, nil
	}
	msg := ""
	for id, dir := range f.Projects {
		marker := ""
		if id == f.Default {
			marker = " (default)"
		}
		msg += fmt.Sprintf("- %s%s -> %s\n", id, marker, dir)
	}
	return textResult(msg), f, nil
}

type rememberArgs struct {
	projectRef
	Agent string   `json:"agent" jsonschema:"your agent name (recorded as the author)"`
	Key   string   `json:"key" jsonschema:"short identifier, letters/digits/dot/dash/underscore, e.g. db-choice"`
	Value string   `json:"value" jsonschema:"the fact/decision to remember (max 16KB; put large content in a file and remember its path)"`
	Tags  []string `json:"tags,omitempty" jsonschema:"optional labels for filtering, e.g. [decision, api]"`
}

func handleRemember(ctx context.Context, req *mcp.CallToolRequest, args rememberArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
	entry, err := o.Remember(args.Agent, args.Key, args.Value, args.Tags)
	if err != nil {
		return errResult(err), nil, nil
	}
	return textResult(fmt.Sprintf("remembered %q (author: %s)", entry.Key, entry.AuthorAgent)), entry, nil
}

type recallArgs struct {
	projectRef
	Agent string `json:"agent,omitempty" jsonschema:"your own agent name — pass it so teammates can see you online"`
	Key   string `json:"key,omitempty" jsonschema:"exact key to fetch; omit to list"`
	Tag   string `json:"tag,omitempty" jsonschema:"only list entries with this tag"`
	Query string `json:"query,omitempty" jsonschema:"only list entries whose key or value contains this text"`
}

func handleRecall(ctx context.Context, req *mcp.CallToolRequest, args recallArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
	entries, err := o.Recall(args.Agent, args.Key, args.Tag, args.Query)
	if err != nil {
		return errResult(err), nil, nil
	}
	if len(entries) == 0 {
		return textResult("no shared memories yet"), entries, nil
	}
	msg := ""
	for _, e := range entries {
		msg += fmt.Sprintf("## %s (by %s, updated %s", e.Key, e.AuthorAgent, e.UpdatedAt.Format("2006-01-02 15:04"))
		if len(e.Tags) > 0 {
			msg += fmt.Sprintf(", tags: %v", e.Tags)
		}
		msg += ")\n" + e.Value + "\n\n"
	}
	return textResult(msg), entries, nil
}

type statsArgs struct {
	projectRef
}

func handleStats(ctx context.Context, req *mcp.CallToolRequest, args statsArgs) (*mcp.CallToolResult, any, error) {
	o, err := args.resolve()
	if err != nil {
		return errResult(err), nil, nil
	}
	stats, err := o.Stats()
	if err != nil {
		return errResult(err), nil, nil
	}
	msg := fmt.Sprintf("project: %s\ntotal handoffs: %d\nquick returns (<30m back to sender): %d\n",
		stats.ProjectID, stats.TotalHandoffs, stats.QuickReturns)
	for _, a := range stats.PerAgent {
		msg += fmt.Sprintf("- %s: sent %d, received %d", a.Agent, a.Sent, a.Received)
		if a.AvgHoldMinutes > 0 {
			msg += fmt.Sprintf(", avg hold %.1f min", a.AvgHoldMinutes)
		}
		msg += "\n"
	}
	for stage, n := range stats.PerStage {
		msg += fmt.Sprintf("  stage %s: %d\n", stage, n)
	}
	return textResult(msg), stats, nil
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
