package model

import (
	"testing"
	"time"
)

func validEnvelope() Envelope {
	return Envelope{
		ProjectID:    "ai-trading-hub",
		CurrentStage: "coding",
		SourceAgent:  "claude-code",
		TargetAgent:  "antigravity-ide",
		Payload: Payload{
			Task:      "generate architecture diagram",
			Artifacts: []string{"src/train.py"},
		},
		Metadata: Metadata{
			Timestamp: time.Now(),
			Version:   "1.0.0",
		},
	}
}

func TestValidateOK(t *testing.T) {
	e := validEnvelope()
	if err := e.Validate([]string{"planning", "coding", "documenting"}); err != nil {
		t.Fatalf("expected valid envelope, got error: %v", err)
	}
}

func TestValidateMissingTask(t *testing.T) {
	e := validEnvelope()
	e.Payload.Task = ""
	if err := e.Validate(nil); err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestValidateStageNotInSet(t *testing.T) {
	e := validEnvelope()
	e.CurrentStage = "deploying"
	if err := e.Validate([]string{"planning", "coding", "documenting"}); err == nil {
		t.Fatal("expected error for stage not in configured set")
	}
}

func TestValidateSameSourceTarget(t *testing.T) {
	e := validEnvelope()
	e.TargetAgent = e.SourceAgent
	if err := e.Validate(nil); err == nil {
		t.Fatal("expected error when source_agent == target_agent")
	}
}

func TestValidateMissingRequiredFields(t *testing.T) {
	cases := []func(*Envelope){
		func(e *Envelope) { e.ProjectID = "" },
		func(e *Envelope) { e.CurrentStage = "" },
		func(e *Envelope) { e.SourceAgent = "" },
		func(e *Envelope) { e.TargetAgent = "" },
		func(e *Envelope) { e.Metadata.Version = "" },
	}
	for i, mutate := range cases {
		e := validEnvelope()
		mutate(&e)
		if err := e.Validate(nil); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}
