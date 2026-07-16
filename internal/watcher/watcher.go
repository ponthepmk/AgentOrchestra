// Package watcher implements AgentOrchestra's "Automated Artifact Sync":
// it watches a project directory for changes to spec/code files and
// automatically fires a handoff to a designated agent (e.g. a diagram/docs
// generator) — no human has to notice the change and trigger it by hand.
package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
)

// skipDirs are directory names that are never watched — our own bookkeeping,
// VCS metadata, and typical build/dependency noise.
var skipDirs = map[string]bool{
	".ao":          true,
	".git":         true,
	"bin":          true,
	"node_modules": true,
	"vendor":       true,
}

const defaultDebounce = 800 * time.Millisecond

// Config controls what a Watcher watches and who it hands off to.
type Config struct {
	Dir         string        // project root to watch (must already be `ao init`-ed)
	SourceAgent string        // agent attributed as the sender of the auto-handoff
	TargetAgent string        // agent to hand off to when a matching file changes
	Stage       string        // stage to set on the handoff; empty keeps the current stage
	Patterns    []string      // glob patterns matched against file basenames; empty means []string{"*.md"}
	Debounce    time.Duration // coalesce rapid-fire events per file; <= 0 means 800ms
}

// Watcher watches a project directory and automatically hands off to
// TargetAgent whenever a file matching Patterns is created or written.
type Watcher struct {
	cfg Config
	fsw *fsnotify.Watcher
	orc *orchestrator.Orchestrator

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// New creates a Watcher and starts watching cfg.Dir (recursively, skipping
// .ao/.git/bin/node_modules/vendor and any other dot-directory).
func New(cfg Config) (*Watcher, error) {
	if len(cfg.Patterns) == 0 {
		cfg.Patterns = []string{"*.md"}
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = defaultDebounce
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fs watcher: %w", err)
	}
	w := &Watcher{
		cfg:    cfg,
		fsw:    fsw,
		orc:    orchestrator.New(cfg.Dir),
		timers: make(map[string]*time.Timer),
	}
	if err := w.addRecursive(cfg.Dir); err != nil {
		fsw.Close()
		return nil, fmt.Errorf("watch %s: %w", cfg.Dir, err)
	}
	return w, nil
}

// Patterns returns the effective glob patterns this Watcher matches files
// against (after defaults have been applied).
func (w *Watcher) Patterns() []string {
	return w.cfg.Patterns
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

// Run blocks, dispatching handoffs as matching files change, until ctx is
// canceled or the underlying watcher is closed.
func (w *Watcher) Run(ctx context.Context, log *slog.Logger) error {
	defer w.fsw.Close()
	log.Info("watch started", "dir", w.cfg.Dir, "patterns", w.cfg.Patterns, "source_agent", w.cfg.SourceAgent, "target_agent", w.cfg.TargetAgent)

	for {
		select {
		case <-ctx.Done():
			log.Info("watch stopped")
			return nil
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ev, log)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			log.Error("watch: fsnotify error", "error", err)
		}
	}
}

func (w *Watcher) handleEvent(ev fsnotify.Event, log *slog.Logger) {
	if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) {
		return
	}

	if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
		base := filepath.Base(ev.Name)
		if skipDirs[base] || strings.HasPrefix(base, ".") {
			return
		}
		if err := w.fsw.Add(ev.Name); err != nil {
			log.Error("watch: add new directory failed", "path", ev.Name, "error", err)
		}
		return
	}

	if !w.matches(filepath.Base(ev.Name)) {
		return
	}
	w.debounce(ev.Name, func() { w.triggerHandoff(ev.Name, log) })
}

func (w *Watcher) matches(name string) bool {
	for _, pattern := range w.cfg.Patterns {
		if ok, _ := filepath.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// debounce coalesces repeated events for the same file (editors often emit
// several Write events per save) into a single handoff.
func (w *Watcher) debounce(key string, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, exists := w.timers[key]; exists {
		t.Stop()
	}
	w.timers[key] = time.AfterFunc(w.cfg.Debounce, fn)
}

func (w *Watcher) triggerHandoff(absPath string, log *slog.Logger) {
	rel, err := filepath.Rel(w.cfg.Dir, absPath)
	if err != nil {
		rel = absPath
	}
	task := fmt.Sprintf("artifact changed: %s — regenerate diagram/docs to match", rel)

	env, err := w.orc.Handoff(orchestrator.HandoffRequest{
		SourceAgent: w.cfg.SourceAgent,
		TargetAgent: w.cfg.TargetAgent,
		Stage:       w.cfg.Stage,
		Task:        task,
		Artifacts:   []string{rel},
	})
	if err != nil {
		log.Error("watch: auto-handoff failed", "path", rel, "error", err)
		return
	}
	log.Info("watch: auto-handoff sent", "path", rel, "target_agent", env.TargetAgent, "stage", env.CurrentStage)
}
