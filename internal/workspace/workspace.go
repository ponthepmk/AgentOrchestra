// Package workspace manages a project's .ao/ directory and .agentconfig
// file — the file-based source of truth that agents hand context through.
package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	aoDir          = ".ao"
	handoffsDir    = "handoffs"
	stateFile      = "state.json"
	agentConfigYML = ".agentconfig"
)

// AgentConfig is the project-level identity file agents read to learn who
// else is plugged into the workspace and which stages the project defines.
type AgentConfig struct {
	ProjectID string   `yaml:"project_id"`
	Agents    []string `yaml:"agents"`
	Stages    []string `yaml:"stages"`
}

// State is the current handoff state of a project: whose turn it is, and
// what they were last asked to do.
type State struct {
	ProjectID    string    `json:"project_id"`
	CurrentStage string    `json:"current_stage"`
	HolderAgent  string    `json:"holder_agent"`
	LastTask     string    `json:"last_task"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Workspace wraps a single project directory's .ao/ state.
type Workspace struct {
	Dir string // project root
}

// Open returns a Workspace for dir without requiring .ao/ to already exist.
func Open(dir string) *Workspace {
	return &Workspace{Dir: dir}
}

func (w *Workspace) aoPath(parts ...string) string {
	return filepath.Join(append([]string{w.Dir, aoDir}, parts...)...)
}

// Initialized reports whether Init has already been run in this workspace.
// It checks for state.json specifically (not just the .ao/ directory)
// because other incidental activity — e.g. the debug logger creating
// .ao/logs/ — can create the .ao/ directory without ever running Init.
func (w *Workspace) Initialized() bool {
	_, err := os.Stat(w.aoPath(stateFile))
	return err == nil
}

// Init creates .ao/ (with handoffs/ and an empty state.json) and a starter
// .agentconfig at the project root. It is safe to call on an already
// initialized workspace (idempotent, does not overwrite .agentconfig).
func (w *Workspace) Init(projectID string, agents, stages []string) error {
	if err := os.MkdirAll(w.aoPath(handoffsDir), 0o755); err != nil {
		return fmt.Errorf("create .ao/handoffs: %w", err)
	}

	if _, err := os.Stat(w.aoPath(stateFile)); os.IsNotExist(err) {
		st := State{
			ProjectID:    projectID,
			CurrentStage: firstOr(stages, "planning"),
			HolderAgent:  firstOr(agents, ""),
			UpdatedAt:    time.Now().UTC(),
		}
		if err := w.WriteState(st); err != nil {
			return err
		}
	}

	cfgPath := filepath.Join(w.Dir, agentConfigYML)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := AgentConfig{ProjectID: projectID, Agents: agents, Stages: stages}
		if err := w.writeAgentConfig(cfg); err != nil {
			return err
		}
	}
	return nil
}

func firstOr(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

func (w *Workspace) writeAgentConfig(cfg AgentConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal .agentconfig: %w", err)
	}
	return atomicWrite(filepath.Join(w.Dir, agentConfigYML), data)
}

// LoadAgentConfig reads .agentconfig from the project root. If the file does
// not exist it returns a zero-value AgentConfig and no error.
func (w *Workspace) LoadAgentConfig() (AgentConfig, error) {
	var cfg AgentConfig
	data, err := os.ReadFile(filepath.Join(w.Dir, agentConfigYML))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read .agentconfig: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse .agentconfig: %w", err)
	}
	return cfg, nil
}

// ReadState loads the current state.json.
func (w *Workspace) ReadState() (State, error) {
	var st State
	data, err := os.ReadFile(w.aoPath(stateFile))
	if err != nil {
		return st, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("parse state: %w", err)
	}
	return st, nil
}

// WriteState atomically overwrites state.json.
func (w *Workspace) WriteState(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.MkdirAll(w.aoPath(), 0o755); err != nil {
		return err
	}
	return atomicWrite(w.aoPath(stateFile), data)
}

var handoffFileRe = regexp.MustCompile(`^(\d+)-`)

// nextHandoffSeq scans handoffs/ for the highest NNN- prefix and returns the
// next sequence number (starting at 1 for an empty directory).
func (w *Workspace) nextHandoffSeq() (int, error) {
	entries, err := os.ReadDir(w.aoPath(handoffsDir))
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, fmt.Errorf("list handoffs: %w", err)
	}
	max := 0
	for _, e := range entries {
		m := handoffFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err == nil && n > max {
			max = n
		}
	}
	return max + 1, nil
}

// WriteHandoff validates and persists env as the next handoff file, then
// updates state.json to reflect the new holder/stage. It returns the
// relative path (from the project root) of the file it wrote.
func (w *Workspace) WriteHandoff(env model.Envelope) (string, error) {
	seq, err := w.nextHandoffSeq()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%03d-%s-to-%s.json", seq, sanitize(env.SourceAgent), sanitize(env.TargetAgent))
	relPath := filepath.Join(aoDir, handoffsDir, name)

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal envelope: %w", err)
	}
	if err := os.MkdirAll(w.aoPath(handoffsDir), 0o755); err != nil {
		return "", err
	}
	if err := atomicWrite(w.aoPath(handoffsDir, name), data); err != nil {
		return "", err
	}

	st := State{
		ProjectID:    env.ProjectID,
		CurrentStage: env.CurrentStage,
		HolderAgent:  env.TargetAgent,
		LastTask:     env.Payload.Task,
		UpdatedAt:    time.Now().UTC(),
	}
	if err := w.WriteState(st); err != nil {
		return "", err
	}
	return relPath, nil
}

func sanitize(agent string) string {
	return strings.ReplaceAll(strings.ToLower(agent), " ", "_")
}

// ListHandoffs returns all handoff envelopes in ascending (chronological)
// order, read directly from .ao/handoffs/ — used to rebuild the SQLite index.
func (w *Workspace) ListHandoffs() ([]HandoffFile, error) {
	entries, err := os.ReadDir(w.aoPath(handoffsDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list handoffs: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := make([]HandoffFile, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(w.aoPath(handoffsDir, name))
		if err != nil {
			return nil, fmt.Errorf("read handoff %s: %w", name, err)
		}
		var env model.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("parse handoff %s: %w", name, err)
		}
		out = append(out, HandoffFile{
			RelPath:  filepath.Join(aoDir, handoffsDir, name),
			Envelope: env,
		})
	}
	return out, nil
}

// HandoffFile pairs a parsed Envelope with the file it was read from.
type HandoffFile struct {
	RelPath  string
	Envelope model.Envelope
}

// atomicWrite writes data to path via a temp file + rename so readers never
// observe a partially written file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
