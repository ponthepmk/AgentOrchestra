// Package registry keeps a machine-level map of project IDs to their
// directories (~/.config/ao/projects.json), so agents whose MCP config is
// machine-wide (e.g. Claude Desktop) can address projects by short ID —
// or omit the project entirely when there's a default — instead of typing
// absolute paths into every tool call.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const fileName = "projects.json"

// File is the on-disk registry format.
type File struct {
	Projects map[string]string `json:"projects"`          // id -> absolute project dir
	Default  string            `json:"default,omitempty"` // id used when a call names no project
}

// dir returns the config directory, honoring AO_CONFIG_DIR (used by tests).
func dir() (string, error) {
	if d := os.Getenv("AO_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, "ao"), nil
}

// Load reads the registry, returning an empty one if it doesn't exist yet.
func Load() (File, error) {
	var f File
	d, err := dir()
	if err != nil {
		return f, err
	}
	data, err := os.ReadFile(filepath.Join(d, fileName))
	if os.IsNotExist(err) {
		return File{Projects: map[string]string{}}, nil
	}
	if err != nil {
		return f, fmt.Errorf("read registry: %w", err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parse registry: %w", err)
	}
	if f.Projects == nil {
		f.Projects = map[string]string{}
	}
	return f, nil
}

func save(f File) error {
	d, err := dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, fileName), data, 0o644)
}

// Register records id -> projectDir. The first project ever registered
// becomes the default automatically.
func Register(id, projectDir string) error {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	f, err := Load()
	if err != nil {
		return err
	}
	f.Projects[id] = abs
	if f.Default == "" {
		f.Default = id
	}
	return save(f)
}

// Resolve turns (projectID, projectDir) tool/CLI inputs into a concrete
// directory:
//   - projectDir given -> used as-is (original behavior, always wins)
//   - projectID given  -> looked up in the registry
//   - neither given    -> the registry's default project, if one exists
func Resolve(projectID, projectDir string) (string, error) {
	if projectDir != "" {
		return projectDir, nil
	}
	f, err := Load()
	if err != nil {
		return "", err
	}
	if projectID != "" {
		if dir, ok := f.Projects[projectID]; ok {
			return dir, nil
		}
		return "", fmt.Errorf("project %q not registered; known projects: %s", projectID, knownIDs(f))
	}
	if f.Default != "" {
		if dir, ok := f.Projects[f.Default]; ok {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no project specified and no default registered; known projects: %s (run `ao init` in a project first)", knownIDs(f))
}

func knownIDs(f File) string {
	if len(f.Projects) == 0 {
		return "(none)"
	}
	ids := make([]string, 0, len(f.Projects))
	for id := range f.Projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}
