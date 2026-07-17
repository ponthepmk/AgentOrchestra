// Package orchestrator contains the business logic shared by AgentOrchestra's
// CLI and MCP server: it wires together the file-based workspace (source of
// truth) and the SQLite index (derived, rebuildable) behind one API.
package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/logging"
	"github.com/ponthepmk/AgentOrchestra/internal/model"
	"github.com/ponthepmk/AgentOrchestra/internal/presence"
	"github.com/ponthepmk/AgentOrchestra/internal/state"
	"github.com/ponthepmk/AgentOrchestra/internal/store"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

const indexFileName = ".ao/index.db"

// Orchestrator operates on a single project directory.
type Orchestrator struct {
	ws  *workspace.Workspace
	dir string
}

// New returns an Orchestrator rooted at dir.
func New(dir string) *Orchestrator {
	return &Orchestrator{ws: workspace.Open(dir), dir: dir}
}

func (o *Orchestrator) openIndex() (*store.SQLiteStore, error) {
	return store.OpenSQLite(o.dir + "/" + indexFileName)
}

// Init sets up .ao/ and .agentconfig for a new project.
func (o *Orchestrator) Init(projectID string, agents []workspace.Agent, stages []string) error {
	log, closeLog := logging.Open(o.dir)
	defer closeLog()
	log.Info("init requested", "project_id", projectID, "agents", len(agents), "stages", stages)

	if err := o.ws.Init(projectID, agents, stages); err != nil {
		log.Error("init failed", "error", err)
		return err
	}
	idx, err := o.openIndex()
	if err != nil {
		log.Error("init: open index failed", "error", err)
		return fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()
	if err := o.reindexWith(idx); err != nil {
		log.Error("init: reindex failed", "error", err)
		return err
	}
	log.Info("init complete", "project_id", projectID)
	return nil
}

// Status is the human/agent-facing snapshot of a project's current state.
type Status struct {
	ProjectID    string
	CurrentStage string
	HolderAgent  string
	LastTask     string
	LastNotes    string
	UpdatedAt    time.Time
	Stages       []string
	Agents       []string
}

// Status reads the current workspace state (file-based, always authoritative).
func (o *Orchestrator) Status() (Status, error) {
	if !o.ws.Initialized() {
		return Status{}, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}

	log, closeLog := logging.Open(o.dir)
	defer closeLog()

	st, err := o.ws.ReadState()
	if err != nil {
		log.Error("status: read state failed", "error", err)
		return Status{}, err
	}
	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		log.Error("status: load agentconfig failed", "error", err)
		return Status{}, err
	}
	log.Debug("status ok", "project_id", st.ProjectID, "stage", st.CurrentStage, "holder_agent", st.HolderAgent)
	return Status{
		ProjectID:    st.ProjectID,
		CurrentStage: st.CurrentStage,
		HolderAgent:  st.HolderAgent,
		LastTask:     st.LastTask,
		LastNotes:    st.LastNotes,
		UpdatedAt:    st.UpdatedAt,
		Stages:       cfg.Stages,
		Agents:       cfg.AgentNames(),
	}, nil
}

// AgentInfo is one team member plus their live presence.
type AgentInfo struct {
	workspace.Agent
	Online   bool      `json:"online"`
	LastSeen time.Time `json:"last_seen,omitzero"`
}

// Agents returns the project's team roster (from .agentconfig) with each
// member's capabilities and current presence — this is what a newly
// connected agent calls to learn who else is on the team and who's around.
func (o *Orchestrator) Agents() ([]AgentInfo, error) {
	if !o.ws.Initialized() {
		return nil, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		return nil, err
	}
	out := make([]AgentInfo, len(cfg.Agents))
	for i, a := range cfg.Agents {
		rec := presence.Get(o.dir, a.Name)
		out[i] = AgentInfo{Agent: a, Online: rec.Online(), LastSeen: rec.LastSeen}
	}
	return out, nil
}

// TouchPresence records that agent was just active (best-effort — never
// fails the caller). via names the operation that saw the agent.
func (o *Orchestrator) TouchPresence(agent, via string) {
	if agent == "" || !o.ws.Initialized() {
		return
	}
	if err := presence.Touch(o.dir, agent, via); err != nil {
		log, closeLog := logging.Open(o.dir)
		defer closeLog()
		log.Error("presence touch failed", "agent", agent, "error", err)
	}
}

// HandoffRequest is the input to Handoff; ProjectID/Metadata are filled in
// automatically from the workspace when left zero.
type HandoffRequest struct {
	SourceAgent string
	TargetAgent string
	Stage       string
	Task        string
	Notes       string
	Artifacts   []string
	Extra       map[string]any
}

// Handoff validates and persists a handoff: writes the envelope to
// .ao/handoffs/, updates .ao/state.json, and indexes it into SQLite.
func (o *Orchestrator) Handoff(req HandoffRequest) (model.Envelope, error) {
	var env model.Envelope
	if !o.ws.Initialized() {
		return env, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}

	log, closeLog := logging.Open(o.dir)
	defer closeLog()
	log.Info("handoff requested", "source_agent", req.SourceAgent, "target_agent", req.TargetAgent, "stage", req.Stage, "task", req.Task)

	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		log.Error("handoff: load agentconfig failed", "error", err)
		return env, err
	}
	if !cfg.HasAgent(req.TargetAgent) {
		err := fmt.Errorf("target_agent %q is not on this project's team (known agents: %s) — check `ao agents` or add it to .agentconfig",
			req.TargetAgent, strings.Join(cfg.AgentNames(), ", "))
		log.Error("handoff rejected", "error", err)
		return env, err
	}
	sm := state.NewMachine(cfg.Stages)

	stage := req.Stage
	if stage == "" {
		st, err := o.ws.ReadState()
		if err != nil {
			log.Error("handoff: read state failed", "error", err)
			return env, err
		}
		stage = st.CurrentStage
	}

	env = model.Envelope{
		ProjectID:    cfg.ProjectID,
		CurrentStage: stage,
		SourceAgent:  req.SourceAgent,
		TargetAgent:  req.TargetAgent,
		Payload: model.Payload{
			Task:      req.Task,
			Notes:     req.Notes,
			Artifacts: req.Artifacts,
			Extra:     req.Extra,
		},
		Metadata: model.Metadata{
			Timestamp: time.Now().UTC(),
			Version:   "1.0.0",
		},
	}

	if err := env.Validate(sm.Stages()); err != nil {
		wrapped := fmt.Errorf("invalid handoff: %w", err)
		log.Error("handoff validation failed", "error", wrapped)
		return env, wrapped
	}

	relPath, err := o.ws.WriteHandoff(env)
	if err != nil {
		log.Error("handoff: write failed", "error", err)
		return env, err
	}

	idx, err := o.openIndex()
	if err != nil {
		wrapped := fmt.Errorf("open index: %w", err)
		log.Error("handoff: open index failed", "error", wrapped)
		return env, wrapped
	}
	defer idx.Close()
	if err := idx.SaveHandoff(store.Entry{PayloadFile: relPath, Envelope: env}); err != nil {
		wrapped := fmt.Errorf("index handoff: %w", err)
		log.Error("handoff: index failed", "error", wrapped)
		return env, wrapped
	}

	// Best-effort side effects: presence heartbeat for the sender, and the
	// human/non-MCP-readable HANDOFF.md mirror. Neither may fail the handoff.
	o.TouchPresence(env.SourceAgent, "handoff")
	if err := o.writeHandoffMirror(); err != nil {
		log.Error("handoff: write HANDOFF.md failed", "error", err)
	}

	log.Info("handoff complete", "project_id", env.ProjectID, "file", relPath, "stage", env.CurrentStage)
	return env, nil
}

// MirrorFileName is the generated human-readable snapshot at the project
// root. It is a one-way mirror: always regenerated, never read back.
const MirrorFileName = "HANDOFF.md"

// writeHandoffMirror regenerates HANDOFF.md from the current state and the
// most recent handoffs, so humans and agents without MCP can just read it.
func (o *Orchestrator) writeHandoffMirror() error {
	st, err := o.ws.ReadState()
	if err != nil {
		return err
	}
	files, err := o.ws.ListHandoffs()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("<!-- generated by AgentOrchestra (ao) — do not edit; every handoff overwrites this file -->\n")
	fmt.Fprintf(&b, "# Handoff — %s\n\n", st.ProjectID)
	fmt.Fprintf(&b, "- **stage:** %s\n", st.CurrentStage)
	fmt.Fprintf(&b, "- **ถือไม้อยู่ (your turn):** %s\n", st.HolderAgent)
	fmt.Fprintf(&b, "- **task:** %s\n", st.LastTask)
	fmt.Fprintf(&b, "- **updated:** %s\n", st.UpdatedAt.Format(time.RFC3339))

	if len(files) > 0 {
		last := files[len(files)-1].Envelope
		if len(last.Payload.Artifacts) > 0 {
			b.WriteString("- **artifacts:**\n")
			for _, a := range last.Payload.Artifacts {
				fmt.Fprintf(&b, "  - `%s`\n", a)
			}
		}
		if last.Payload.Notes != "" {
			b.WriteString("\n## Notes จากผู้ส่ง\n\n")
			b.WriteString(last.Payload.Notes)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n## ประวัติล่าสุด\n\n")
	start := len(files) - 5
	if start < 0 {
		start = 0
	}
	for i := len(files) - 1; i >= start; i-- {
		env := files[i].Envelope
		fmt.Fprintf(&b, "- [%s] %s → %s (%s): %s\n",
			env.Metadata.Timestamp.Format("2006-01-02 15:04"), env.SourceAgent, env.TargetAgent, env.CurrentStage, env.Payload.Task)
	}

	return o.ws.WriteRootFile(MirrorFileName, []byte(b.String()))
}

// LogFilter narrows down Log results; zero values mean "no filter".
type LogFilter struct {
	Stage string
	Agent string
	Limit int
}

// Log returns indexed handoff history, most recent first, narrowed by filter.
func (o *Orchestrator) Log(filter LogFilter) ([]store.HandoffRecord, error) {
	if !o.ws.Initialized() {
		return nil, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}

	log, closeLog := logging.Open(o.dir)
	defer closeLog()

	idx, err := o.openIndex()
	if err != nil {
		log.Error("log: open index failed", "error", err)
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		log.Error("log: load agentconfig failed", "error", err)
		return nil, err
	}
	records, err := idx.ListHandoffs(cfg.ProjectID, store.ListFilter{Stage: filter.Stage, Agent: filter.Agent, Limit: filter.Limit})
	if err != nil {
		log.Error("log: list handoffs failed", "error", err)
		return nil, err
	}
	log.Debug("log ok", "project_id", cfg.ProjectID, "count", len(records), "stage_filter", filter.Stage, "agent_filter", filter.Agent)
	return records, nil
}

// Reindex rebuilds the SQLite index from the .ao/handoffs/ files on disk —
// the recovery path if index.db is deleted, corrupted, or out of sync.
func (o *Orchestrator) Reindex() error {
	if !o.ws.Initialized() {
		return fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}

	log, closeLog := logging.Open(o.dir)
	defer closeLog()

	idx, err := o.openIndex()
	if err != nil {
		log.Error("reindex: open index failed", "error", err)
		return fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()
	if err := o.reindexWith(idx); err != nil {
		log.Error("reindex failed", "error", err)
		return err
	}
	log.Info("reindex complete")
	return nil
}

func (o *Orchestrator) reindexWith(idx *store.SQLiteStore) error {
	files, err := o.ws.ListHandoffs()
	if err != nil {
		return err
	}
	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		return err
	}
	entries := make([]store.Entry, len(files))
	for i, f := range files {
		entries[i] = store.Entry{PayloadFile: f.RelPath, Envelope: f.Envelope}
	}
	return idx.Reindex(cfg.ProjectID, entries)
}
