// Package cli implements the human-facing `ao` subcommands (init, status,
// handoff, log). They call the same orchestrator package the MCP server
// uses, so behavior is identical whether a human or an agent drives it.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
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
	case "handoff":
		err = runHandoff(rest, stdout)
	case "log":
		err = runLog(rest, stdout)
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
  ao init    --id <project_id> [--agents a,b,c] [--stages s1,s2,s3] [dir]
  ao status  [dir]
  ao handoff --from <agent> --to <agent> --task "<what to do>" [--stage <stage>] [--artifact <path>]... [dir]
  ao log     [--limit N] [--reindex] [dir]
  ao mcp     — run as an MCP server over stdio (for agents, not humans)

[dir] defaults to the current directory.
`)
}

// stringSlice implements flag.Value to collect repeated --artifact flags.
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

func runInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	id := fs.String("id", "", "project id (required)")
	agents := fs.String("agents", "", "comma-separated agent names, e.g. claude-code,antigravity-ide")
	stages := fs.String("stages", "", "comma-separated stage names, defaults to planning,coding,documenting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArg(fs.Args())
	if *id == "" {
		return fmt.Errorf("--id is required")
	}

	o := orchestrator.New(dir)
	if err := o.Init(*id, splitCSV(*agents), splitCSV(*stages)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "initialized AgentOrchestra workspace for %q in %s\n", *id, dir)
	return nil
}

func runStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArg(fs.Args())

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
	return nil
}

func runHandoff(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	from := fs.String("from", "", "source agent (required)")
	to := fs.String("to", "", "target agent (required)")
	task := fs.String("task", "", "what the target agent should do (required)")
	stage := fs.String("stage", "", "stage to move to; defaults to the current stage")
	var artifacts stringSlice
	fs.Var(&artifacts, "artifact", "relevant file path (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArg(fs.Args())

	if *from == "" || *to == "" || *task == "" {
		return fmt.Errorf("--from, --to, and --task are required")
	}

	o := orchestrator.New(dir)
	env, err := o.Handoff(orchestrator.HandoffRequest{
		SourceAgent: *from,
		TargetAgent: *to,
		Stage:       *stage,
		Task:        *task,
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
	limit := fs.Int("limit", 0, "max number of handoffs to show (0 = all)")
	reindex := fs.Bool("reindex", false, "rebuild the SQLite index from .ao/handoffs/ before listing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := dirArg(fs.Args())

	o := orchestrator.New(dir)
	if *reindex {
		if err := o.Reindex(); err != nil {
			return fmt.Errorf("reindex: %w", err)
		}
	}
	records, err := o.Log(*limit)
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

func dirArg(positional []string) string {
	if len(positional) > 0 && positional[0] != "" {
		return positional[0]
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
