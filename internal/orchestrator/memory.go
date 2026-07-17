package orchestrator

import (
	"fmt"

	"github.com/ponthepmk/AgentOrchestra/internal/logging"
	"github.com/ponthepmk/AgentOrchestra/internal/memory"
)

// Remember upserts a shared-memory entry authored by agent.
func (o *Orchestrator) Remember(agent, key, value string, tags []string) (memory.Entry, error) {
	if !o.ws.Initialized() {
		return memory.Entry{}, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	log, closeLog := logging.Open(o.dir)
	defer closeLog()

	entry := memory.Entry{Key: key, Value: value, AuthorAgent: agent, Tags: tags}
	if err := memory.Put(o.dir, entry); err != nil {
		log.Error("remember failed", "key", key, "agent", agent, "error", err)
		return memory.Entry{}, err
	}
	o.TouchPresence(agent, "remember")
	log.Info("memory saved", "key", key, "agent", agent, "tags", tags)

	saved, err := memory.Get(o.dir, key)
	if err != nil {
		return memory.Entry{}, err
	}
	return saved, nil
}

// Recall fetches one entry by key, or lists entries by tag/query when key
// is empty. agent (optional) marks the caller online.
func (o *Orchestrator) Recall(agent, key, tag, query string) ([]memory.Entry, error) {
	if !o.ws.Initialized() {
		return nil, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	o.TouchPresence(agent, "recall")

	if key != "" {
		e, err := memory.Get(o.dir, key)
		if err != nil {
			return nil, err
		}
		return []memory.Entry{e}, nil
	}
	return memory.List(o.dir, memory.Filter{Tag: tag, Query: query})
}

// Forget removes a shared-memory entry.
func (o *Orchestrator) Forget(key string) error {
	if !o.ws.Initialized() {
		return fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	log, closeLog := logging.Open(o.dir)
	defer closeLog()
	if err := memory.Remove(o.dir, key); err != nil {
		return err
	}
	log.Info("memory removed", "key", key)
	return nil
}
