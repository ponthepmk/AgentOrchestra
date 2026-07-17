// Package cli implements the human-facing `ao` subcommands. They call the
// same orchestrator package the MCP server uses, so behavior is identical
// whether a human or an agent drives it.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/logging"
	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
	"github.com/ponthepmk/AgentOrchestra/internal/registry"
	"github.com/ponthepmk/AgentOrchestra/internal/scaffold"
	"github.com/ponthepmk/AgentOrchestra/internal/state"
	"github.com/ponthepmk/AgentOrchestra/internal/watcher"
	"github.com/ponthepmk/AgentOrchestra/internal/worker"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

// Run dispatches args (os.Args[1:] minus the "mcp" subcommand, which main
// handles separately) to the matching subcommand. It returns the process
// exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	sub, rest := args[0], args[1:]
	var err error
	switch sub {
	case "init":
		err = runInit(rest, stdout)
	case "status":
		err = runStatus(rest, stdout)
	case "agents":
		err = runAgents(rest, stdout)
	case "projects":
		err = runProjects(rest, stdout)
	case "handoff":
		err = runHandoff(rest, stdout)
	case "log":
		err = runLog(rest, stdout)
	case "watch":
		err = runWatch(rest, stdout)
	case "worker":
		err = runWorker(rest, stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "ao: unknown command %q\n\n", sub)
		printUsage(stderr)
		return 2
	}

	if err != nil {
		fmt.Fprintf(stderr, "ao: %v\n", err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `AgentOrchestra (ao) — Multi-Agent context handoff

Usage:
  ao init     --id <project_id> [--preset team] [--agents a,b,c] [--stages s1,s2,s3] [dir]
  ao status   [--project <id>] [dir]
  ao agents   [--project <id>] [dir]                    — team roster, capabilities, who's online
  ao projects                                           — projects registered on this machine
  ao handoff  --from <agent> --to <agent> --task "..." [--note "..."] [--stage <stage>] [--artifact <path>]... [--project <id>] [dir]
  ao log      [--limit N] [--stage <stage>] [--agent <agent>] [--reindex] [--project <id>] [dir]
  ao watch    --from <agent> --to <agent> [--pattern <glob>]... [--stage <stage>] [--notify] [--project <id>] [dir]
  ao worker   --agent <name> --url <openai-compatible-base-url> --model <model> [--api-key K] [--poll 5s] [--handoff-back] [--project <id>] [dir]
  ao mcp      — run as an MCP server over stdio (for agents, not humans)

Project selection: pass a [dir], or --project <registered id>, or nothing at
all to use the default project (the first one you ran "ao init" in).
Every run appends debug details to .ao/logs/ao.log in the target project.
`)
}

// stringSlice implements flag.Value to collect repeated flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// resolveDir turns (--project, positional dir) into a concrete directory.
// An explicit positional dir wins; then a --project lookup; then the
// registry default.
func resolveDir(project string, positional []string) (string, error) {
	if len(positional) > 0 && positional[0] != "" {
		return positional[0], nil
	}
	if project != "" {
		return registry.Resolve(project, "")
	}
	// A cwd that is an initialized workspace beats the registry default,
	// so `ao status` inside a project always means *this* project.
	if wd, err := os.Getwd(); err == nil {
		if workspace.Open(wd).Initialized() {
			return wd, nil
		}
	}
	if dir, err := registry.Resolve("", ""); err == nil {
		return dir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return ".", nil
	}
	return wd, nil
}

func runInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	id := fs.String("id", "", "project id (required)")
	preset := fs.String("preset", "", `"team" seeds the standard roster: claude-code, codex, antigravity-ide, ollama-worker`)
	agents := fs.String("agents", "", "comma-separated agent names, e.g. claude-code,antigravity-ide")
	stages := fs.String("stages", "", "comma-separated stage names, defaults to planning,coding,documenting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArgOrCwd(fs.Args())
	if *id == "" {
		return fmt.Errorf("--id is required")
	}

	var team []workspace.Agent
	switch {
	case *preset == "team":
		team = scaffold.TeamPreset()
	case *agents != "":
		team = workspace.AgentsFromNames(splitCSV(*agents))
	default:
		team = scaffold.TeamPreset()
	}

	stageList := splitCSV(*stages)
	if len(stageList) == 0 {
		stageList = state.DefaultStages
	}

	o := orchestrator.New(dir)
	if err := o.Init(*id, team, stageList); err != nil {
		return err
	}
	if err := scaffold.Apply(dir, team); err != nil {
		return fmt.Errorf("scaffold: %w", err)
	}
	if err := registry.Register(*id, dir); err != nil {
		return fmt.Errorf("register project: %w", err)
	}

	fmt.Fprintf(stdout, "initialized AgentOrchestra workspace for %q in %s\n", *id, dir)
	fmt.Fprintf(stdout, "  ✔ .agentconfig (%d agents) — แก้ทีม/ความถนัดได้ที่ไฟล์นี้\n", len(team))
	fmt.Fprintf(stdout, "  ✔ .mcp.json (Claude Code / OpenCode อ่านอัตโนมัติ)\n")
	fmt.Fprintf(stdout, "  ✔ CLAUDE.md + AGENTS.md (กติกาให้ agent เรียก ao_* เอง)\n")
	fmt.Fprintf(stdout, "  ✔ ลงทะเบียนโปรเจกต์แล้ว — เรียกใช้จากที่ไหนก็ได้ด้วย --project %s\n\n", *id)
	fmt.Fprintf(stdout, "สำหรับ Claude Desktop วาง config นี้ใน claude_desktop_config.json แล้ว restart แอป:\n%s\n", scaffold.ClaudeDesktopSnippet())
	return nil
}

func runStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	project := fs.String("project", "", "registered project id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := resolveDir(*project, fs.Args())
	if err != nil {
		return err
	}

	o := orchestrator.New(dir)
	st, err := o.Status()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "project:      %s\n", st.ProjectID)
	fmt.Fprintf(stdout, "stage:        %s\n", st.CurrentStage)
	fmt.Fprintf(stdout, "holder_agent: %s\n", st.HolderAgent)
	fmt.Fprintf(stdout, "last_task:    %s\n", st.LastTask)
	fmt.Fprintf(stdout, "updated_at:   %s\n", st.UpdatedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(stdout, "stages:       %s\n", strings.Join(st.Stages, ", "))
	fmt.Fprintf(stdout, "agents:       %s\n", strings.Join(st.Agents, ", "))
	if st.LastNotes != "" {
		fmt.Fprintf(stdout, "notes:\n%s\n", st.LastNotes)
	}
	return nil
}

func runAgents(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	project := fs.String("project", "", "registered project id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := resolveDir(*project, fs.Args())
	if err != nil {
		return err
	}

	o := orchestrator.New(dir)
	infos, err := o.Agents()
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Fprintln(stdout, "no agents configured — add them to .agentconfig")
		return nil
	}
	for _, a := range infos {
		status := "⚪ offline"
		if a.Online {
			status = "🟢 online"
		} else if !a.LastSeen.IsZero() {
			status = "⚪ last seen " + a.LastSeen.Format("2006-01-02 15:04") + " UTC"
		}
		fmt.Fprintf(stdout, "%-18s %s", a.Name, status)
		if len(a.Capabilities) > 0 {
			fmt.Fprintf(stdout, "  [%s]", strings.Join(a.Capabilities, ", "))
		}
		if a.Description != "" {
			fmt.Fprintf(stdout, "  — %s", a.Description)
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func runProjects(args []string, stdout io.Writer) error {
	f, err := registry.Load()
	if err != nil {
		return err
	}
	if len(f.Projects) == 0 {
		fmt.Fprintln(stdout, "no projects registered yet — run `ao init` in a project first")
		return nil
	}
	for id, dir := range f.Projects {
		marker := "  "
		if id == f.Default {
			marker = "* "
		}
		fmt.Fprintf(stdout, "%s%-24s %s\n", marker, id, dir)
	}
	fmt.Fprintln(stdout, "(* = default project when none is specified)")
	return nil
}

func runHandoff(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	project := fs.String("project", "", "registered project id")
	from := fs.String("from", "", "source agent (required)")
	to := fs.String("to", "", "target agent (required)")
	task := fs.String("task", "", "what the target agent should do (required)")
	note := fs.String("note", "", "longer free-form context for the target agent (markdown ok)")
	stage := fs.String("stage", "", "stage to move to; defaults to the current stage")
	var artifacts stringSlice
	fs.Var(&artifacts, "artifact", "relevant file path (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := resolveDir(*project, fs.Args())
	if err != nil {
		return err
	}

	if *from == "" || *to == "" || *task == "" {
		return fmt.Errorf("--from, --to, and --task are required")
	}

	o := orchestrator.New(dir)
	env, err := o.Handoff(orchestrator.HandoffRequest{
		SourceAgent: *from,
		TargetAgent: *to,
		Stage:       *stage,
		Task:        *task,
		Notes:       *note,
		Artifacts:   artifacts,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "handed off %s -> %s (stage: %s): %s\n", env.SourceAgent, env.TargetAgent, env.CurrentStage, env.Payload.Task)
	return nil
}

func runLog(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	project := fs.String("project", "", "registered project id")
	limit := fs.Int("limit", 0, "max number of handoffs to show (0 = all)")
	stage := fs.String("stage", "", "only show handoffs at this stage")
	agent := fs.String("agent", "", "only show handoffs where this agent is the source or the target")
	reindex := fs.Bool("reindex", false, "rebuild the SQLite index from .ao/handoffs/ before listing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := resolveDir(*project, fs.Args())
	if err != nil {
		return err
	}

	o := orchestrator.New(dir)
	if *reindex {
		if err := o.Reindex(); err != nil {
			return fmt.Errorf("reindex: %w", err)
		}
	}
	records, err := o.Log(orchestrator.LogFilter{Stage: *stage, Agent: *agent, Limit: *limit})
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintln(stdout, "no handoffs recorded yet")
		return nil
	}
	for _, r := range records {
		fmt.Fprintf(stdout, "[%s] %s -> %s (%s): %s\n",
			r.CreatedAt.Format("2006-01-02T15:04:05Z"), r.SourceAgent, r.TargetAgent, r.Stage, r.Task)
	}
	return nil
}

// runWatch implements Automated Artifact Sync: it blocks, watching dir for
// changes to files matching --pattern, and automatically records a handoff
// to --to whenever one changes — until interrupted (Ctrl+C / SIGTERM).
func runWatch(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	project := fs.String("project", "", "registered project id")
	from := fs.String("from", "", "agent to attribute automatic handoffs to (required)")
	to := fs.String("to", "", "agent to hand off to when a watched file changes (required)")
	stage := fs.String("stage", "", "stage to set on auto-handoffs; defaults to the current stage")
	notifyFlag := fs.Bool("notify", false, "send a desktop notification on each auto-handoff")
	var patterns stringSlice
	fs.Var(&patterns, "pattern", "glob pattern to watch, matched against file basenames (repeatable, default: *.md)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := resolveDir(*project, fs.Args())
	if err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return fmt.Errorf("--from and --to are required")
	}

	w, err := watcher.New(watcher.Config{
		Dir:         dir,
		SourceAgent: *from,
		TargetAgent: *to,
		Stage:       *stage,
		Patterns:    patterns,
		Notify:      *notifyFlag,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "watching %s for changes to %v — auto-handoff %s -> %s (Ctrl+C to stop)\n", dir, w.Patterns(), *from, *to)

	log, closeLog := logging.Open(dir)
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return w.Run(ctx, log)
}

// runWorker drives a local LLM (Ollama / LM Studio / llama.cpp / vLLM — any
// OpenAI-compatible server) as a team member: it polls for handoffs
// addressed to --agent, executes them against the model, and optionally
// hands the result back.
func runWorker(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	project := fs.String("project", "", "registered project id")
	agent := fs.String("agent", "", "agent name this worker acts as (required)")
	url := fs.String("url", "", "OpenAI-compatible base URL, e.g. http://localhost:11434/v1 (required)")
	model := fs.String("model", "", "model name to request (required)")
	apiKey := fs.String("api-key", "", "API key if the server needs one")
	poll := fs.Duration("poll", 5*time.Second, "how often to check for new work")
	handoffBack := fs.Bool("handoff-back", true, "hand the result back to the sender when done")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := resolveDir(*project, fs.Args())
	if err != nil {
		return err
	}
	if *agent == "" || *url == "" || *model == "" {
		return fmt.Errorf("--agent, --url, and --model are required")
	}

	wk := worker.New(worker.Config{
		Dir:         dir,
		Agent:       *agent,
		BaseURL:     *url,
		Model:       *model,
		APIKey:      *apiKey,
		Poll:        *poll,
		HandoffBack: *handoffBack,
	})

	fmt.Fprintf(stdout, "worker %q online — model %s @ %s, polling every %s (Ctrl+C to stop)\n", *agent, *model, *url, *poll)

	log, closeLog := logging.Open(dir)
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return wk.Run(ctx, log)
}

// dirArgOrCwd is init's simpler resolution: positional dir or cwd (init
// must never fall back to the registry default — it creates new projects).
func dirArgOrCwd(positional []string) string {
	if len(positional) > 0 && positional[0] != "" {
		return positional[0]
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
