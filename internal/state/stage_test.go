package state

import "testing"

func TestDefaultStages(t *testing.T) {
	m := NewMachine(nil)
	for _, s := range DefaultStages {
		if !m.Valid(s) {
			t.Fatalf("expected default stage %q to be valid", s)
		}
	}
	if m.Valid("deploying") {
		t.Fatal("expected 'deploying' to be invalid under default stages")
	}
}

func TestCustomStages(t *testing.T) {
	m := NewMachine([]string{"research", "build", "release"})
	if !m.Valid("build") {
		t.Fatal("expected custom stage 'build' to be valid")
	}
	if m.Valid("coding") {
		t.Fatal("expected default stage 'coding' to be invalid for custom set")
	}
}

func TestCanTransition(t *testing.T) {
	m := NewMachine([]string{"planning", "coding", "documenting"})
	if err := m.CanTransition("planning", "coding"); err != nil {
		t.Fatalf("expected valid forward transition, got %v", err)
	}
	if err := m.CanTransition("documenting", "coding"); err != nil {
		t.Fatalf("expected valid backward transition, got %v", err)
	}
	if err := m.CanTransition("coding", "deploying"); err == nil {
		t.Fatal("expected error transitioning to unconfigured stage")
	}
	if err := m.CanTransition("deploying", "coding"); err == nil {
		t.Fatal("expected error transitioning from unconfigured stage")
	}
}
