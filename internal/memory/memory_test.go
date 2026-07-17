package memory

import (
	"strings"
	"testing"
)

func TestPutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	err := Put(dir, Entry{Key: "db-choice", Value: "ใช้ SQLite เพราะ home lab", AuthorAgent: "claude-code", Tags: []string{"decision"}})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	e, err := Get(dir, "db-choice")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if e.Value != "ใช้ SQLite เพราะ home lab" || e.AuthorAgent != "claude-code" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestPutUpsertsSameKey(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(Put(dir, Entry{Key: "k", Value: "old", AuthorAgent: "a"}))
	must(Put(dir, Entry{Key: "k", Value: "new", AuthorAgent: "b"}))
	e, err := Get(dir, "k")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if e.Value != "new" || e.AuthorAgent != "b" {
		t.Fatalf("expected upsert to overwrite, got %+v", e)
	}
	all, _ := List(dir, Filter{})
	if len(all) != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", len(all))
	}
}

func TestPutValidation(t *testing.T) {
	dir := t.TempDir()
	if err := Put(dir, Entry{Key: "../evil", Value: "x", AuthorAgent: "a"}); err == nil {
		t.Fatal("expected path-unsafe key to be rejected")
	}
	if err := Put(dir, Entry{Key: "k", Value: "", AuthorAgent: "a"}); err == nil {
		t.Fatal("expected empty value to be rejected")
	}
	if err := Put(dir, Entry{Key: "k", Value: "x", AuthorAgent: ""}); err == nil {
		t.Fatal("expected missing author to be rejected")
	}
	big := strings.Repeat("x", MaxValueBytes+1)
	if err := Put(dir, Entry{Key: "k", Value: big, AuthorAgent: "a"}); err == nil {
		t.Fatal("expected oversized value to be rejected")
	}
}

func TestListFilters(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(Put(dir, Entry{Key: "db-choice", Value: "SQLite", AuthorAgent: "a", Tags: []string{"decision"}}))
	must(Put(dir, Entry{Key: "api-style", Value: "MCP tools", AuthorAgent: "b", Tags: []string{"decision", "api"}}))
	must(Put(dir, Entry{Key: "gotcha-1", Value: "watcher must skip HANDOFF.md", AuthorAgent: "a", Tags: []string{"gotcha"}}))

	byTag, err := List(dir, Filter{Tag: "decision"})
	if err != nil {
		t.Fatalf("List by tag failed: %v", err)
	}
	if len(byTag) != 2 {
		t.Fatalf("expected 2 decision entries, got %d", len(byTag))
	}

	byQuery, err := List(dir, Filter{Query: "handoff"})
	if err != nil {
		t.Fatalf("List by query failed: %v", err)
	}
	if len(byQuery) != 1 || byQuery[0].Key != "gotcha-1" {
		t.Fatalf("unexpected query result: %+v", byQuery)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	if err := Put(dir, Entry{Key: "k", Value: "x", AuthorAgent: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, "k"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := Get(dir, "k"); err == nil {
		t.Fatal("expected Get to fail after Remove")
	}
	if err := Remove(dir, "k"); err == nil {
		t.Fatal("expected Remove of missing key to error")
	}
}

func TestListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := List(dir, Filter{})
	if err != nil || entries != nil {
		t.Fatalf("expected empty result for fresh dir, got %v / %v", entries, err)
	}
	if Count(dir) != 0 {
		t.Fatal("expected Count 0")
	}
}
