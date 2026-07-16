// Package orchestrator contains the business logic shared by AgentOrchestra's
// CLI and MCP server: it wires together the file-based workspace (source of
// truth) and the SQLite index (derived, rebuildable) behind one API.
package orchestrator

import (
	"fmt"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/logging"
	"github.com/ponthepmk/AgentOrchestra/internal/model"
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
func (o *Orchestrator) Init(projectID string, agents, stages []string) error {
	log, closeLog := logging.Open(o.dir)
	defer closeLog()
	log.Info("init requested", "project_id", projectID, "agents", agents, "stages", stages)

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
		UpdatedAt:    st.UpdatedAt,
		Stages:       cfg.Stages,
		Agents:       cfg.Agents,
	}, nil
}

// HandoffRequest is the input to Handoff; ProjectID/Metadata are filled in
// automatically from the workspace when left zero.
type HandoffRequest struct {
	SourceAgent string
	TargetAgent string
	Stage       string
	Task        string
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
	log.Info("handoff complete", "project_id", env.ProjectID, "file", relPath, "stage", env.CurrentStage)
	return env, nil
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
