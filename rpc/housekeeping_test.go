package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type hkResult struct {
	Since           string   `json:"since"`
	ChangedRecently []string `json:"changed_recently"`
	Thresholds      struct {
		ObservationsOverCap []struct {
			Path         string         `json:"path"`
			Entries      int            `json:"entries"`
			Cap          int            `json:"cap"`
			ByPrimaryTag map[string]int `json:"by_primary_tag"`
		} `json:"observations_over_cap"`
		CompletedActionsOverCap []struct {
			Path      string `json:"path"`
			Completed int    `json:"completed"`
			Cap       int    `json:"cap"`
		} `json:"completed_actions_over_cap"`
		ImprovementsImplementedOverCap []struct {
			Path        string `json:"path"`
			Implemented int    `json:"implemented"`
			Cap         int    `json:"cap"`
		} `json:"improvements_implemented_over_cap"`
		HotMemoryOverCap []struct {
			Path  string `json:"path"`
			Lines int    `json:"lines"`
			Cap   int    `json:"cap"`
		} `json:"hot_memory_over_cap"`
		PatternsOverCap []struct {
			Path     string `json:"path"`
			Lines    int    `json:"lines"`
			Size     int64  `json:"size"`
			LinesCap int    `json:"lines_cap"`
			SizeCap  int64  `json:"size_cap"`
		} `json:"patterns_over_cap"`
	} `json:"thresholds"`
	DormantDomains []struct {
		ID              string `json:"id"`
		LastObservation string `json:"last_observation"`
	} `json:"dormant_domains"`
	StaleActionItems []struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Text    string `json:"text"`
		Added   string `json:"added"`
		AgeDays int    `json:"age_days"`
	} `json:"stale_action_items"`
}

func mustWriteFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func callHK(t *testing.T, ts *testServer, role string) hkResult {
	t.Helper()
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 99, Method: "housekeeping_scan",
		Params: map[string]interface{}{"role": role},
	})
	if resp.Error != nil {
		t.Fatalf("housekeeping_scan error: %v", resp.Error.Message)
	}
	var r hkResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return r
}

// Missing role -> CodeInvalidParams. Mirrors open_actions contract.
func TestHousekeepingScanMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "housekeeping_scan",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("want invalid params, got %+v", resp.Error)
	}
}

// Empty store -> envelope returns with all arrays non-nil and no thresholds.
func TestHousekeepingScanEmptyEnvelope(t *testing.T) {
	ts := newTestServer(t)
	r := callHK(t, ts, "siona")
	if r.ChangedRecently == nil || r.DormantDomains == nil || r.StaleActionItems == nil {
		t.Fatalf("nil slices in envelope: %+v", r)
	}
	if len(r.Thresholds.ObservationsOverCap) != 0 ||
		len(r.Thresholds.CompletedActionsOverCap) != 0 ||
		len(r.Thresholds.HotMemoryOverCap) != 0 ||
		len(r.Thresholds.PatternsOverCap) != 0 ||
		len(r.Thresholds.ImprovementsImplementedOverCap) != 0 {
		t.Fatalf("expected no threshold violations, got %+v", r.Thresholds)
	}
}

// observations > 50 -> reported with by_primary_tag aggregation. This is
// the load-bearing win documented in RPC-CONSOLIDATION.md §2.
func TestHousekeepingScanObservationsOverCapAggregatesPrimaryTag(t *testing.T) {
	ts := newTestServer(t)
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "- 2026-05-01 [health, body]: line")
	}
	for i := 0; i < 25; i++ {
		lines = append(lines, "- 2026-05-02 [milestone]: line")
	}
	mustWriteFile(t, ts.memoryRoot, "personal/observations.md",
		"# Observations\n\n"+strings.Join(lines, "\n")+"\n")

	r := callHK(t, ts, "siona")
	if len(r.Thresholds.ObservationsOverCap) != 1 {
		t.Fatalf("want 1 obs violation, got %d: %+v", len(r.Thresholds.ObservationsOverCap), r.Thresholds.ObservationsOverCap)
	}
	v := r.Thresholds.ObservationsOverCap[0]
	if v.Path != "personal/observations.md" || v.Entries != 55 || v.Cap != 50 {
		t.Fatalf("bad violation: %+v", v)
	}
	if v.ByPrimaryTag["health"] != 30 || v.ByPrimaryTag["milestone"] != 25 {
		t.Fatalf("primary-tag aggregation wrong: %+v", v.ByPrimaryTag)
	}
}

// completed-actions cap + stale open item: both detected from one file.
func TestHousekeepingScanActionItemsCompletedAndStale(t *testing.T) {
	ts := newTestServer(t)
	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines, "- [x] done thing")
	}
	old := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	lines = append(lines, "- [ ] revive backups | added:"+old+" | pri:high")
	fresh := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	lines = append(lines, "- [ ] write doc | added:"+fresh)
	mustWriteFile(t, ts.memoryRoot, "personal/action-items.md", strings.Join(lines, "\n")+"\n")

	r := callHK(t, ts, "siona")
	if len(r.Thresholds.CompletedActionsOverCap) != 1 ||
		r.Thresholds.CompletedActionsOverCap[0].Completed != 12 ||
		r.Thresholds.CompletedActionsOverCap[0].Cap != 10 {
		t.Fatalf("bad completed: %+v", r.Thresholds.CompletedActionsOverCap)
	}
	if len(r.StaleActionItems) != 1 {
		t.Fatalf("want 1 stale, got %d: %+v", len(r.StaleActionItems), r.StaleActionItems)
	}
	if r.StaleActionItems[0].Text != "revive backups" || r.StaleActionItems[0].AgeDays < 28 {
		t.Fatalf("bad stale item: %+v", r.StaleActionItems[0])
	}
}

// hot-memory line cap, root and per-domain both honored.
func TestHousekeepingScanHotMemoryOverCap(t *testing.T) {
	ts := newTestServer(t)
	bigBody := strings.Repeat("line\n", 60)
	mustWriteFile(t, ts.memoryRoot, "hot-memory.md", bigBody)
	mustWriteFile(t, ts.memoryRoot, "personal/hot-memory.md", bigBody)

	r := callHK(t, ts, "siona")
	paths := map[string]int{}
	for _, v := range r.Thresholds.HotMemoryOverCap {
		paths[v.Path] = v.Lines
	}
	if paths["hot-memory.md"] != 60 || paths["personal/hot-memory.md"] != 60 {
		t.Fatalf("hot-memory caps not detected: %+v", paths)
	}
}

// patterns.md dual cap; test the byte trip with under-threshold line count.
func TestHousekeepingScanPatternsOverCapByBytes(t *testing.T) {
	ts := newTestServer(t)
	body := strings.Repeat("x", 6000) + "\n"
	mustWriteFile(t, ts.memoryRoot, "cog-meta/patterns.md", body)
	r := callHK(t, ts, "siona")
	if len(r.Thresholds.PatternsOverCap) != 1 {
		t.Fatalf("want patterns violation, got %+v", r.Thresholds.PatternsOverCap)
	}
	v := r.Thresholds.PatternsOverCap[0]
	if v.Size <= v.SizeCap {
		t.Fatalf("expected size > cap: %+v", v)
	}
}

// Dormant domain: observations.md exists but has no entry in 28-day window.
// Dakota has no obs file -> stays invisible; work gets a 60-day-old entry.
func TestHousekeepingScanDormantDomain(t *testing.T) {
	ts := newTestServer(t)
	old := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	mustWriteFile(t, ts.memoryRoot, "work/microsoft/observations.md",
		"- "+old+" [milestone]: ancient win\n")

	r := callHK(t, ts, "siona")
	var found bool
	for _, d := range r.DormantDomains {
		if d.ID == "work" {
			found = true
			if d.LastObservation != old {
				t.Fatalf("dormant last_observation = %q, want %q", d.LastObservation, old)
			}
		}
	}
	if !found {
		t.Fatalf("work domain not flagged dormant: %+v", r.DormantDomains)
	}
}

// RBAC: project-reader can't see personal/. Threshold violations under
// personal/ must be filtered out, AND changed_recently must hide the path
// — otherwise the gate leaks paths via metadata.
func TestHousekeepingScanRBACFilters(t *testing.T) {
	ts := newTestServer(t)
	var lines []string
	for i := 0; i < 55; i++ {
		lines = append(lines, "- 2026-05-01 [health]: x")
	}
	mustWriteFile(t, ts.memoryRoot, "personal/observations.md", strings.Join(lines, "\n")+"\n")

	r := callHK(t, ts, "project-reader")
	for _, v := range r.Thresholds.ObservationsOverCap {
		if strings.HasPrefix(v.Path, "personal/") {
			t.Fatalf("project-reader saw personal/ threshold: %+v", v)
		}
	}
	for _, p := range r.ChangedRecently {
		if strings.HasPrefix(p, "personal/") {
			t.Fatalf("project-reader saw personal/ path in changed_recently: %q", p)
		}
	}
}

// improvements.md cap, using the cog-meta path that the RPC bakes in.
func TestHousekeepingScanImprovementsOverCap(t *testing.T) {
	ts := newTestServer(t)
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, "- [x] shipped thing")
	}
	mustWriteFile(t, ts.memoryRoot, "cog-meta/improvements.md", strings.Join(lines, "\n")+"\n")

	r := callHK(t, ts, "siona")
	if len(r.Thresholds.ImprovementsImplementedOverCap) != 1 ||
		r.Thresholds.ImprovementsImplementedOverCap[0].Implemented != 15 {
		t.Fatalf("bad improvements result: %+v", r.Thresholds.ImprovementsImplementedOverCap)
	}
}

// changed_recently: marker mtime drives the window. Files modified after
// the marker show up; older ones don't. The marker file itself is excluded.
func TestHousekeepingScanChangedRecentlyHonorsMarker(t *testing.T) {
	ts := newTestServer(t)
	mustWriteFile(t, ts.memoryRoot, "personal/entities.md", "old\n")
	twoDaysAgo := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(ts.memoryRoot, "personal/entities.md"), twoDaysAgo, twoDaysAgo); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, ts.memoryRoot, "personal/hot-memory.md", "fresh\n")
	mustWriteFile(t, ts.memoryRoot, "cog-meta/.housekeeping-marker", "")
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(filepath.Join(ts.memoryRoot, "cog-meta/.housekeeping-marker"), oneHourAgo, oneHourAgo); err != nil {
		t.Fatal(err)
	}

	r := callHK(t, ts, "siona")
	if r.Since == "" {
		t.Fatal("since empty — marker present, RFC3339 expected")
	}
	seen := map[string]bool{}
	for _, p := range r.ChangedRecently {
		seen[p] = true
	}
	if !seen["personal/hot-memory.md"] {
		t.Fatalf("fresh file missing from changed_recently: %v", r.ChangedRecently)
	}
	if seen["personal/entities.md"] {
		t.Fatalf("stale file leaked into changed_recently: %v", r.ChangedRecently)
	}
	if seen["cog-meta/.housekeeping-marker"] {
		t.Fatalf("marker file should not appear in changed_recently")
	}
}
