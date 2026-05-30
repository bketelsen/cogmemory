package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bketelsen/cogmemory/store"
)

func writeObs(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newClusterStore(t *testing.T) (*store.MemoryStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestClusterByTag(t *testing.T) {
	s, dir := newClusterStore(t)
	writeObs(t, dir, "personal/observations.md", strings.Join([]string{
		"- 2026-05-12 [health, sleep]: 4 hours, headache next day",
		"- 2026-05-15 [health]: low energy after lunch",
		"- 2026-05-20 [health, food]: skipped breakfast again",
		"- 2026-05-29 [health]: better sleep last 3 nights",
		"- 2026-05-29 [work]: shipped open_actions RPC",
		"- 2026-05-29 [work]: domain controller landed",
		"- 2026-05-29 [work]: cluster_check started",
	}, "\n")+"\n")

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	res, err := s.Cluster(
		[]store.ClusterObsTarget{{Domain: "personal", Path: "personal/observations.md"}},
		store.ClusterParams{Since: now.AddDate(0, 0, -30), Now: now},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ByTag) < 2 {
		t.Fatalf("expected >=2 tag clusters, got %d: %+v", len(res.ByTag), res.ByTag)
	}
	if res.ByTag[0].Tag != "health" || res.ByTag[0].Count != 4 {
		t.Fatalf("expected health=4 first, got %+v", res.ByTag[0])
	}
	if res.ByTag[0].SpansDays < 17 {
		t.Fatalf("expected health span ~18 days, got %d", res.ByTag[0].SpansDays)
	}
	if len(res.ByTag[0].Samples) == 0 {
		t.Fatalf("expected samples")
	}
	if res.ByTag[0].Samples[0].Date != "2026-05-29" {
		t.Fatalf("expected newest sample first, got %s", res.ByTag[0].Samples[0].Date)
	}
}

func TestClusterSinceFilters(t *testing.T) {
	s, dir := newClusterStore(t)
	writeObs(t, dir, "personal/observations.md", strings.Join([]string{
		"- 2026-04-01 [old]: way back",
		"- 2026-04-02 [old]: also old",
		"- 2026-04-03 [old]: ancient",
		"- 2026-05-28 [fresh]: recent one",
		"- 2026-05-29 [fresh]: another",
		"- 2026-05-29 [fresh]: third",
	}, "\n")+"\n")

	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	res, _ := s.Cluster(
		[]store.ClusterObsTarget{{Domain: "personal", Path: "personal/observations.md"}},
		store.ClusterParams{Since: now.AddDate(0, 0, -7), Now: now},
	)
	for _, c := range res.ByTag {
		if c.Tag == "old" {
			t.Fatalf("old should be filtered by since, got %+v", c)
		}
	}
}

func TestClusterByKeyword(t *testing.T) {
	s, dir := newClusterStore(t)
	writeObs(t, dir, "infra/observations.md", strings.Join([]string{
		"- 2026-05-20 [infra]: kanban dispatcher crashed again",
		"- 2026-05-21 [infra]: kanban DB still flaky",
		"- 2026-05-22 [infra]: kanban locking improved",
		"- 2026-05-23 [infra]: unrelated note about disk",
	}, "\n")+"\n")

	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	res, _ := s.Cluster(
		[]store.ClusterObsTarget{{Domain: "infra", Path: "infra/observations.md"}},
		store.ClusterParams{Since: now.AddDate(0, 0, -30), Now: now, MinClusterSize: 3},
	)
	found := false
	for _, c := range res.ByKeyword {
		if c.Keyword == "kanban" && c.Count == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kanban keyword cluster, got %+v", res.ByKeyword)
	}
}

func TestClusterThreadCandidates(t *testing.T) {
	s, dir := newClusterStore(t)
	writeObs(t, dir, "personal/observations.md", strings.Join([]string{
		"- 2026-04-15 [health]: started glp1",
		"- 2026-04-22 [health]: down 4 lbs",
		"- 2026-05-29 [health]: down 12 lbs total",
	}, "\n")+"\n")

	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	res, _ := s.Cluster(
		[]store.ClusterObsTarget{{Domain: "personal", Path: "personal/observations.md"}},
		store.ClusterParams{Since: now.AddDate(0, 0, -60), Now: now, SpanDays: 14},
	)
	if len(res.ThreadCandidates) == 0 {
		t.Fatalf("expected thread candidate, got none")
	}
	found := false
	for _, tc := range res.ThreadCandidates {
		if tc.Topic == "tag:health" && tc.FragmentCount == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected tag:health thread, got %+v", res.ThreadCandidates)
	}
}

func TestClusterMinSizeRespected(t *testing.T) {
	s, dir := newClusterStore(t)
	now := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	writeObs(t, dir, "x/observations.md", "- 2026-05-29 [x]: one\n")
	res, err := s.Cluster(
		[]store.ClusterObsTarget{{Domain: "x", Path: "x/observations.md"}},
		store.ClusterParams{Now: now},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ByTag) != 0 {
		t.Fatalf("expected no clusters, got %+v", res.ByTag)
	}
}

func TestClusterMissingFileSkipped(t *testing.T) {
	s, _ := newClusterStore(t)
	_, err := s.Cluster(
		[]store.ClusterObsTarget{{Domain: "x", Path: "x/observations.md"}},
		store.ClusterParams{},
	)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
}
