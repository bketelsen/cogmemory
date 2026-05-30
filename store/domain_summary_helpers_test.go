package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecentObservationsFiltersAndParses(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	body := `- 2026-05-28 [milestone,personal]: shipped
- 2026-04-01 [chore]: stale
- not an obs line
- 2026-05-29 [tag]: today
`
	if err := os.WriteFile(filepath.Join(root, "observations.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	obs, err := s.RecentObservations("observations.md", "2026-05-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 {
		t.Fatalf("want 2, got %d: %+v", len(obs), obs)
	}
	if obs[0].Date != "2026-05-28" || len(obs[0].Tags) != 2 || obs[0].Tags[0] != "milestone" {
		t.Errorf("first obs wrong: %+v", obs[0])
	}

	// Missing file → empty, no error.
	got, err := s.RecentObservations("nope.md", "")
	if err != nil || len(got) != 0 {
		t.Errorf("missing file: got=%v err=%v", got, err)
	}
}

func TestCountActionsHandlesFencesAndDates(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	body := "- [ ] one\n" +
		"- [ ] two\n" +
		"- [x] done recent | added:2026-05-28\n" +
		"- [x] done old | added:2026-01-01\n" +
		"- [x] done undated\n" +
		"```\n- [ ] inside code fence (skip)\n```\n" +
		"<!-- - [ ] in comment (skip) -->\n"
	if err := os.WriteFile(filepath.Join(root, "ai.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	open, completed, err := s.CountActions("ai.md", "2026-05-15")
	if err != nil {
		t.Fatal(err)
	}
	if open != 2 {
		t.Errorf("open=%d, want 2", open)
	}
	if completed != 1 {
		t.Errorf("completed_since=%d, want 1", completed)
	}

	// since="" counts every completed.
	_, completedAll, _ := s.CountActions("ai.md", "")
	if completedAll != 3 {
		t.Errorf("completedAll=%d, want 3", completedAll)
	}

	// Missing file → zeros.
	o, c, err := s.CountActions("nope.md", "")
	if err != nil || o != 0 || c != 0 {
		t.Errorf("missing: o=%d c=%d err=%v", o, c, err)
	}
}

func TestFileExistsAndModTime(t *testing.T) {
	root := t.TempDir()
	s, _ := New(root)
	if ok, _ := s.FileExists("nope.md"); ok {
		t.Errorf("missing file reported as existing")
	}
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.FileExists("x.md"); !ok {
		t.Errorf("existing file reported as missing")
	}
	mt, err := s.FileModTime("x.md")
	if err != nil || mt.IsZero() {
		t.Errorf("mtime: got=%v err=%v", mt, err)
	}
	zt, err := s.FileModTime("nope.md")
	if err != nil || !zt.IsZero() {
		t.Errorf("missing mtime: got=%v err=%v", zt, err)
	}
}
