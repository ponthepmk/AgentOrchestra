// Package state implements the project stage machine (planning -> coding ->
// documenting by default, or whatever stages a project's .agentconfig defines).
package state

import "fmt"

// DefaultStages is used when a project's .agentconfig does not define any.
var DefaultStages = []string{"planning", "coding", "documenting"}

// Machine enforces stage membership and transitions for a single project.
type Machine struct {
	stages []string
}

// NewMachine builds a Machine from a project's configured stage list.
// An empty list falls back to DefaultStages.
func NewMachine(stages []string) *Machine {
	if len(stages) == 0 {
		stages = DefaultStages
	}
	return &Machine{stages: stages}
}

// Stages returns the ordered list of valid stage names.
func (m *Machine) Stages() []string {
	out := make([]string, len(m.stages))
	copy(out, m.stages)
	return out
}

// Valid reports whether stage is one of the machine's configured stages.
func (m *Machine) Valid(stage string) bool {
	for _, s := range m.stages {
		if s == stage {
			return true
		}
	}
	return false
}

// CanTransition reports whether moving from one stage to another is allowed.
// Any configured stage may move to any other configured stage (forward or
// backward, e.g. sending work back from documenting to coding) — the only
// restriction is that both stages must be part of the project's configured set.
func (m *Machine) CanTransition(from, to string) error {
	if !m.Valid(from) {
		return fmt.Errorf("stage %q is not a configured stage %v", from, m.stages)
	}
	if !m.Valid(to) {
		return fmt.Errorf("stage %q is not a configured stage %v", to, m.stages)
	}
	return nil
}
