package store_test

import (
	"strings"
	"testing"

	"github.com/bketelsen/cogmemory/store"
)

func TestSessionBriefReadsHotMemoryAndPatterns(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "# Hot\nstrategic context\n")
	writeFile(t, dir, "cog-meta/patterns.md", "# Patterns\nrule one\n")

	brief, err := s.SessionBrief(nil)
	if err != nil {
		t.Fatalf("SessionBrief: %v", err)
	}
	if !strings.Contains(brief.HotMemory, "strategic context") {
		t.Errorf("HotMemory = %q, want to contain 'strategic context'", brief.HotMemory)
	}
	if !strings.Contains(brief.Patterns, "rule one") {
		t.Errorf("Patterns = %q, want to contain 'rule one'", brief.Patterns)
	}
	if brief.DomainActionCounts == nil {
		t.Error("DomainActionCounts should be non-nil empty slice")
	}
	if brief.PriHighAnywhere {
		t.Error("PriHighAnywhere should be false with no targets")
	}
}

func TestSessionBriefMissingFilesReturnEmpty(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)

	brief, err := s.SessionBrief(nil)
	if err != nil {
		t.Fatalf("SessionBrief: %v", err)
	}
	if brief.HotMemory != "" || brief.Patterns != "" {
		t.Errorf("missing files should return empty strings, got hot=%q patterns=%q",
			brief.HotMemory, brief.Patterns)
	}
}

func TestSessionBriefCountsOpenActionsPerDomain(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "projects/dakota/action-items.md", strings.Join([]string{
		"- [ ] one | pri:high",
		"- [ ] two | pri:medium",
		"- [x] done",
		"<!--",
		"- [ ] commented",
		"-->",
	}, "\n"))
	writeFile(t, dir, "personal/action-items.md", "- [ ] solo | pri:low\n")

	brief, err := s.SessionBrief([]store.ActionTarget{
		{Domain: "dakota", Path: "projects/dakota/action-items.md"},
		{Domain: "personal", Path: "personal/action-items.md"},
		{Domain: "missing", Path: "no/such/action-items.md"},
	})
	if err != nil {
		t.Fatalf("SessionBrief: %v", err)
	}
	counts := map[string]store.DomainActionCountItem{}
	for _, c := range brief.DomainActionCounts {
		counts[c.Domain] = c
	}
	if c := counts["dakota"]; c.OpenCount != 2 || c.PriHighCount != 1 {
		t.Errorf("dakota = %+v, want open=2 high=1", c)
	}
	if c := counts["personal"]; c.OpenCount != 1 || c.PriHighCount != 0 {
		t.Errorf("personal = %+v, want open=1 high=0", c)
	}
	if c := counts["missing"]; c.OpenCount != 0 {
		t.Errorf("missing target should be 0, got %+v", c)
	}
	if !brief.PriHighAnywhere {
		t.Error("PriHighAnywhere should be true (dakota has pri:high)")
	}
}
