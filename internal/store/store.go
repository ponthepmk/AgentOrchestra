// Package store indexes handoff history for fast querying. The index is
// derived data — the .ao/ files on disk are the source of truth — so any
// Store implementation must support a full Reindex from scratch.
package store

import (
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/model"
)

// Entry pairs a persisted envelope with the workspace-relative path of the
// file it was written to, for indexing.
type Entry struct {
	PayloadFile string
	Envelope    model.Envelope
}

// ProjectRecord is the indexed current state of a project.
type ProjectRecord struct {
	ID           string
	CurrentStage string
	HolderAgent  string
	UpdatedAt    time.Time
}

// HandoffRecord is a single indexed handoff, most recent first when listed.
type HandoffRecord struct {
	ID          int64
	ProjectID   string
	SourceAgent string
	TargetAgent string
	Stage       string
	Task        string
	PayloadFile string
	CreatedAt   time.Time
}

// Store indexes handoff history and per-project state for a workspace.
type Store interface {
	// SaveHandoff indexes a single new handoff and updates the project's
	// current state to reflect it.
	SaveHandoff(entry Entry) error

	// ListHandoffs returns indexed handoffs for a project, most recent
	// first. limit <= 0 means no limit.
	ListHandoffs(projectID string, limit int) ([]HandoffRecord, error)

	// ProjectState returns the indexed current state of a project.
	ProjectState(projectID string) (ProjectRecord, error)

	// Reindex wipes and rebuilds the index for projectID from entries,
	// which must be in chronological (oldest-first) order.
	Reindex(projectID string, entries []Entry) error

	Close() error
}
