// Package model defines the Standard Payload envelope that all agents use
// to hand off context through AgentOrchestra.
package model

import (
	"fmt"
	"time"
)

// Payload carries the actual work request between agents.
type Payload struct {
	Task      string         `json:"task"`
	Artifacts []string       `json:"artifacts,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// Metadata carries bookkeeping information about the handoff itself.
type Metadata struct {
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}

// Envelope is the Standard Payload exchanged between agents via AgentOrchestra.
type Envelope struct {
	ProjectID    string   `json:"project_id"`
	CurrentStage string   `json:"current_stage"`
	SourceAgent  string   `json:"source_agent"`
	TargetAgent  string   `json:"target_agent"`
	Payload      Payload  `json:"payload"`
	Metadata     Metadata `json:"metadata"`
}

// Validate checks that an Envelope has all required fields populated.
// stages is the set of valid stage names for the project (from .agentconfig);
// pass nil to skip stage-membership validation.
func (e Envelope) Validate(stages []string) error {
	if e.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if e.CurrentStage == "" {
		return fmt.Errorf("current_stage is required")
	}
	if e.SourceAgent == "" {
		return fmt.Errorf("source_agent is required")
	}
	if e.TargetAgent == "" {
		return fmt.Errorf("target_agent is required")
	}
	if e.SourceAgent == e.TargetAgent {
		return fmt.Errorf("source_agent and target_agent must differ")
	}
	if e.Payload.Task == "" {
		return fmt.Errorf("payload.task is required")
	}
	if e.Metadata.Version == "" {
		return fmt.Errorf("metadata.version is required")
	}
	if len(stages) > 0 {
		valid := false
		for _, s := range stages {
			if s == e.CurrentStage {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("current_stage %q is not one of the project's configured stages %v", e.CurrentStage, stages)
		}
	}
	return nil
}
