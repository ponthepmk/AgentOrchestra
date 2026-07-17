// Package memory implements cross-agent shared memory: a key-value store
// under .ao/memory/ that lets agents leave knowledge for each other
// (decisions made, gotchas, facts) WITHOUT passing the baton. Files are the
// source of truth and are meant to be committed, so team knowledge travels
// with the repo and has git history when entries get overwritten.
//
// Every entry carries its author and updated_at so readers can judge
// freshness themselves — there is no TTL. Do NOT store secrets here: every
// agent on the project reads these values verbatim.
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxValueBytes caps a single entry's value so the store stays a notebook,
// not a dumping ground (and so recalled values can't blow up a small
// model's context).
const MaxValueBytes = 16 * 1024

const memDir = "memory"

// Entry is one remembered fact.
type Entry struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	AuthorAgent string    `json:"author_agent"`
	Tags        []string  `json:"tags,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var keyRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func dirFor(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".ao", memDir)
}

func fileFor(workspaceDir, key string) string {
	return filepath.Join(dirFor(workspaceDir), key+".json")
}

// ValidateKey enforces filesystem-safe keys.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if !keyRe.MatchString(key) {
		return fmt.Errorf("key %q must contain only letters, digits, dot, dash, underscore", key)
	}
	return nil
}

// Put upserts an entry (same key = overwrite; git history keeps the old
// value when .ao/memory is committed).
func Put(workspaceDir string, e Entry) error {
	if err := ValidateKey(e.Key); err != nil {
		return err
	}
	if e.Value == "" {
		return fmt.Errorf("value is required")
	}
	if len(e.Value) > MaxValueBytes {
		return fmt.Errorf("value is %d bytes; memory entries are capped at %d bytes — store the content in a file and remember its path instead", len(e.Value), MaxValueBytes)
	}
	if e.AuthorAgent == "" {
		return fmt.Errorf("author agent is required so readers know who wrote this")
	}
	e.UpdatedAt = time.Now().UTC()

	if err := os.MkdirAll(dirFor(workspaceDir), 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp := fileFor(workspaceDir, e.Key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write memory entry: %w", err)
	}
	return os.Rename(tmp, fileFor(workspaceDir, e.Key))
}

// Get returns the entry for key.
func Get(workspaceDir, key string) (Entry, error) {
	var e Entry
	if err := ValidateKey(key); err != nil {
		return e, err
	}
	data, err := os.ReadFile(fileFor(workspaceDir, key))
	if os.IsNotExist(err) {
		return e, fmt.Errorf("no memory entry for key %q", key)
	}
	if err != nil {
		return e, fmt.Errorf("read memory entry: %w", err)
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("parse memory entry %q: %w", key, err)
	}
	return e, nil
}

// Remove deletes the entry for key.
func Remove(workspaceDir, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	err := os.Remove(fileFor(workspaceDir, key))
	if os.IsNotExist(err) {
		return fmt.Errorf("no memory entry for key %q", key)
	}
	return err
}

// Filter narrows List results; zero values mean "no filter".
type Filter struct {
	Tag   string // exact tag match
	Query string // case-insensitive substring over key and value
}

// List returns all entries matching filter, most recently updated first.
func List(workspaceDir string, f Filter) ([]Entry, error) {
	entries, err := os.ReadDir(dirFor(workspaceDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list memory dir: %w", err)
	}

	var out []Entry
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dirFor(workspaceDir), de.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if f.Tag != "" && !hasTag(e, f.Tag) {
			continue
		}
		if f.Query != "" && !matchesQuery(e, f.Query) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Count returns how many entries exist (used by ao doctor).
func Count(workspaceDir string) int {
	entries, err := List(workspaceDir, Filter{})
	if err != nil {
		return 0
	}
	return len(entries)
}

func hasTag(e Entry, tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func matchesQuery(e Entry, q string) bool {
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(e.Key), q) || strings.Contains(strings.ToLower(e.Value), q)
}
