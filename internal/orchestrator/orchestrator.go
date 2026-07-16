// Package orchestrator contains the business logic shared by AgentOrchestra's
// CLI and MCP server: it wires together the file-based workspace (source of
// truth) and the SQLite index (derived, rebuildable) behind one API.
package orchestrator

import (
	"fmt"
	"time"

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
	if err := o.ws.Init(projectID, agents, stages); err != nil {
		return err
	}
	idx, err := o.openIndex()
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()
	return o.reindexWith(idx)
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
	st, err := o.ws.ReadState()
	if err != nil {
		return Status{}, err
	}
	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		return Status{}, err
	}
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
	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		return env, err
	}
	sm := state.NewMachine(cfg.Stages)

	stage := req.Stage
	if stage == "" {
		st, err := o.ws.ReadState()
		if err != nil {
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
		return env, fmt.Errorf("invalid handoff: %w", err)
	}

	relPath, err := o.ws.WriteHandoff(env)
	if err != nil {
		return env, err
	}

	idx, err := o.openIndex()
	if err != nil {
		return env, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()
	if err := idx.SaveHandoff(store.Entry{PayloadFile: relPath, Envelope: env}); err != nil {
		return env, fmt.Errorf("index handoff: %w", err)
	}
	return env, nil
}

// Log returns indexed handoff history, most recent first.
func (o *Orchestrator) Log(limit int) ([]store.HandoffRecord, error) {
	if !o.ws.Initialized() {
		return nil, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	idx, err := o.openIndex()
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		return nil, err
	}
	return idx.ListHandoffs(cfg.ProjectID, limit)
}

// Reindex rebuilds the SQLite index from the .ao/handoffs/ files on disk —
// the recovery path if index.db is deleted, corrupted, or out of sync.
func (o *Orchestrator) Reindex() error {
	if !o.ws.Initialized() {
		return fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	idx, err := o.openIndex()
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()
	return o.reindexWith(idx)
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
