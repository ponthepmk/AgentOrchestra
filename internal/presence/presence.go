// Package presence tracks which agents have recently been active in a
// workspace, via per-agent heartbeat files in .ao/presence/. It's how an
// agent connecting to a project can see who else is around right now.
// Presence files are ephemeral machine-local state and are not meant to be
// committed (ao init adds .ao/presence/ to the project's .gitignore).
package presence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OnlineWindow is how recently an agent must have been seen to count as
// online.
const OnlineWindow = 10 * time.Minute

// Record is one agent's last-seen heartbeat.
type Record struct {
	Agent    string    `json:"agent"`
	LastSeen time.Time `json:"last_seen"`
	Via      string    `json:"via"` // what touched it: "handoff", "status", "agents", "worker", ...
}

// Online reports whether the record is fresh enough to count as connected.
func (r Record) Online() bool {
	return time.Since(r.LastSeen) < OnlineWindow
}

func presenceDir(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".ao", "presence")
}

func fileFor(workspaceDir, agent string) string {
	safe := strings.ReplaceAll(strings.ToLower(agent), " ", "_")
	return filepath.Join(presenceDir(workspaceDir), safe+".json")
}

// Touch records that agent was active just now. Best-effort: errors are
// returned but callers are expected to treat them as non-fatal.
func Touch(workspaceDir, agent, via string) error {
	if agent == "" {
		return nil
	}
	if err := os.MkdirAll(presenceDir(workspaceDir), 0o755); err != nil {
		return fmt.Errorf("create presence dir: %w", err)
	}
	rec := Record{Agent: agent, LastSeen: time.Now().UTC(), Via: via}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := fileFor(workspaceDir, agent) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write presence: %w", err)
	}
	return os.Rename(tmp, fileFor(workspaceDir, agent))
}

// Get returns the presence record for agent, or a zero Record (never
// online) if the agent has no heartbeat yet.
func Get(workspaceDir, agent string) Record {
	data, err := os.ReadFile(fileFor(workspaceDir, agent))
	if err != nil {
		return Record{Agent: agent}
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{Agent: agent}
	}
	return rec
}
