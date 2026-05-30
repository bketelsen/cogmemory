package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlacierIndexEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "mem"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.GlacierIndex()
	if err != nil {
		t.Fatalf("GlacierIndex: %v", err)
	}
	if entries == nil {
		t.Fatal("entries should be empty slice, not nil")
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries, got %d", len(entries))
	}
}

func TestGlacierIndexParsesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "mem")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "glacier", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `<!-- L0: Archived completed project action items 2026-05-18 to 2026-05-22 -->
---
type: action-items-done
domain: projects
date_range: 2026-05-18 to 2026-05-22
entries: 14
summary: Completed v0.14.6 through v0.24.4 sprint items
tags: [housekeeping, milestone]
---
# Projects — Completed Action Items
- [x] one
`
	if err := os.WriteFile(filepath.Join(root, "glacier", "projects", "action-items-done.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "glacier", "projects", "orphan.md"), []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "glacier", "projects", "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := s.GlacierIndex()
	if err != nil {
		t.Fatalf("GlacierIndex: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Path != "glacier/projects/action-items-done.md" {
		t.Errorf("entries[0].Path = %q", entries[0].Path)
	}
	e := entries[0]
	if e.Type != "action-items-done" || e.Domain != "projects" ||
		e.DateRange != "2026-05-18 to 2026-05-22" || e.Entries != 14 ||
		e.Summary != "Completed v0.14.6 through v0.24.4 sprint items" {
		t.Errorf("frontmatter parse mismatch: %+v", e)
	}
	if len(e.Tags) != 2 || e.Tags[0] != "housekeeping" || e.Tags[1] != "milestone" {
		t.Errorf("tags = %v", e.Tags)
	}
	o := entries[1]
	if o.Path != "glacier/projects/orphan.md" {
		t.Errorf("orphan path = %q", o.Path)
	}
	if o.Tags == nil {
		t.Error("orphan tags should be empty slice, not nil")
	}
	if o.Type != "" || o.Domain != "" {
		t.Errorf("orphan should have empty frontmatter fields: %+v", o)
	}
}

func TestGlacierIndexSkipsTmp(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "mem")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "glacier"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "glacier", "x.md.tmp"), []byte("---\ntype: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := s.GlacierIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries (tmp skipped), got %d", len(entries))
	}
}
