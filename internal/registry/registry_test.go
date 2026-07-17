package registry

import (
	"strings"
	"testing"
)

func setupConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
}

func TestResolveExplicitDirWins(t *testing.T) {
	setupConfigDir(t)
	dir, err := Resolve("some-id", "/explicit/path")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if dir != "/explicit/path" {
		t.Fatalf("expected explicit dir to win, got %q", dir)
	}
}

func TestRegisterAndResolveByID(t *testing.T) {
	setupConfigDir(t)
	proj := t.TempDir()
	if err := Register("my-proj", proj); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	dir, err := Resolve("my-proj", "")
	if err != nil {
		t.Fatalf("Resolve by id failed: %v", err)
	}
	if dir != proj {
		t.Fatalf("expected %q, got %q", proj, dir)
	}
}

func TestFirstRegisteredBecomesDefault(t *testing.T) {
	setupConfigDir(t)
	first, second := t.TempDir(), t.TempDir()
	if err := Register("first", first); err != nil {
		t.Fatalf("Register first failed: %v", err)
	}
	if err := Register("second", second); err != nil {
		t.Fatalf("Register second failed: %v", err)
	}
	dir, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve default failed: %v", err)
	}
	if dir != first {
		t.Fatalf("expected default to be first project %q, got %q", first, dir)
	}
}

func TestResolveUnknownIDListsKnown(t *testing.T) {
	setupConfigDir(t)
	if err := Register("known", t.TempDir()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	_, err := Resolve("unknown", "")
	if err == nil {
		t.Fatal("expected error for unknown project id")
	}
	if !strings.Contains(err.Error(), "known") {
		t.Fatalf("expected error to list known projects, got: %v", err)
	}
}

func TestResolveNothingRegistered(t *testing.T) {
	setupConfigDir(t)
	if _, err := Resolve("", ""); err == nil {
		t.Fatal("expected error when nothing is registered and no input given")
	}
}
