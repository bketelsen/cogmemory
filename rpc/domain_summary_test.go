package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper to seed common files for the tests below.
func seedSummaryFixtures(t *testing.T, ts *testServer) {
	t.Helper()
	// hot-memory
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/hot-memory.md",
			"content": "personal hot memory body\n",
		},
	})
	// action-items: 2 open, 1 completed in window, 1 completed before window, 1 completed undated.
	actionsBody := `## Open
- [ ] open task one | added:2026-05-25
- [ ] open task two

## Completed
- [x] recent completed | added:2026-05-28
- [x] ancient completed | added:2026-01-01
- [x] undated completed
`
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/action-items.md",
			"content": actionsBody,
		},
	})
	// observations: 1 in window, 1 out of window.
	obsBody := `- 2026-05-28 [milestone,personal]: shipped the thing
- 2026-04-01 [chore]: stale entry
`
	// Bypass observation validator by writing rather than appending.
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/observations.md",
			"content": obsBody,
		},
	})
	// entities: present-but-not-summarized; should appear in files_present.
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 4, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/entities.md",
			"content": "### Sierra\n",
		},
	})
}

type summaryResult struct {
	Domain                    string `json:"domain"`
	Label                     string `json:"label"`
	HotMemory                 string `json:"hot_memory"`
	OpenActionCount           int    `json:"open_action_count"`
	CompletedActionCountSince int    `json:"completed_action_count_since"`
	RecentObservations        []struct {
		Date string   `json:"date"`
		Tags []string `json:"tags"`
		Text string   `json:"text"`
	} `json:"recent_observations"`
	FilesPresent []string `json:"files_present"`
	LastActivity string   `json:"last_activity"`
	Since        string   `json:"since"`
}

func TestDomainSummaryHappyPath(t *testing.T) {
	ts := newTestServer(t)
	seedSummaryFixtures(t, ts)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 10, Method: "domain_summary",
		Params: map[string]interface{}{
			"role":   "siona",
			"domain": "personal",
			"since":  "2026-05-15",
		},
	})
	if resp.Error != nil {
		t.Fatalf("domain_summary: %v", resp.Error.Message)
	}
	var r summaryResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Domain != "personal" || r.Label != "Personal" {
		t.Fatalf("domain/label mismatch: %+v", r)
	}
	if r.HotMemory != "personal hot memory body\n" {
		t.Errorf("hot_memory mismatch: %q", r.HotMemory)
	}
	if r.OpenActionCount != 2 {
		t.Errorf("open_action_count = %d, want 2", r.OpenActionCount)
	}
	if r.CompletedActionCountSince != 1 {
		t.Errorf("completed_action_count_since = %d, want 1 (only added:>=2026-05-15 counts)", r.CompletedActionCountSince)
	}
	if len(r.RecentObservations) != 1 || r.RecentObservations[0].Date != "2026-05-28" {
		t.Errorf("recent_observations filter failed: %+v", r.RecentObservations)
	}
	if len(r.RecentObservations) > 0 {
		tags := r.RecentObservations[0].Tags
		if len(tags) != 2 || tags[0] != "milestone" || tags[1] != "personal" {
			t.Errorf("tag parsing wrong: %+v", tags)
		}
	}
	want := map[string]bool{"hot-memory": true, "action-items": true, "observations": true, "entities": true}
	for _, f := range r.FilesPresent {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("files_present missing: %v (got %v)", want, r.FilesPresent)
	}
	if r.Since != "2026-05-15" {
		t.Errorf("since echo wrong: %q", r.Since)
	}
	if r.LastActivity == "" {
		t.Errorf("last_activity empty, expected non-empty")
	}
}

func TestDomainSummaryDefaultSinceIs7Days(t *testing.T) {
	ts := newTestServer(t)
	seedSummaryFixtures(t, ts)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domain_summary",
		Params: map[string]interface{}{"role": "siona", "domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("domain_summary: %v", resp.Error.Message)
	}
	var r summaryResult
	json.Unmarshal(resp.Result, &r)
	want := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02")
	if r.Since != want {
		t.Errorf("default since = %q, want %q", r.Since, want)
	}
}

func TestDomainSummarySinceDurationForms(t *testing.T) {
	ts := newTestServer(t)
	for _, since := range []string{"7d", "168h", "2026-05-15", "2026-05-15T00:00:00Z"} {
		resp := call(t, ts.socketPath, rpcRequest{
			JSONRPC: "2.0", ID: 1, Method: "domain_summary",
			Params: map[string]interface{}{"role": "siona", "domain": "personal", "since": since},
		})
		if resp.Error != nil {
			t.Fatalf("since %q: %v", since, resp.Error.Message)
		}
	}
	// invalid form rejected
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 99, Method: "domain_summary",
		Params: map[string]interface{}{"role": "siona", "domain": "personal", "since": "garbage"},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected InvalidParams for garbage since, got %+v", resp.Error)
	}
}

func TestDomainSummaryRBACDenied(t *testing.T) {
	ts := newTestServer(t)
	seedSummaryFixtures(t, ts)
	// project-reader has read on projects/** only; personal/** is denied.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domain_summary",
		Params: map[string]interface{}{"role": "project-reader", "domain": "personal"},
	})
	if resp.Error == nil || resp.Error.Code != -32000 {
		t.Fatalf("expected RBAC denied (-32000), got %+v error=%+v", resp.Result, resp.Error)
	}
}

func TestDomainSummaryUnknownDomain(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domain_summary",
		Params: map[string]interface{}{"role": "siona", "domain": "ghost"},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected InvalidParams for unknown domain, got %+v", resp.Error)
	}
}

func TestDomainSummaryMissingFilesAreOmitted(t *testing.T) {
	ts := newTestServer(t)
	// Only seed hot-memory; action-items + observations absent.
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role": "siona", "path": "personal/hot-memory.md", "content": "only this\n",
		},
	})
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "domain_summary",
		Params: map[string]interface{}{"role": "siona", "domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("domain_summary: %v", resp.Error.Message)
	}
	var r summaryResult
	json.Unmarshal(resp.Result, &r)
	if len(r.FilesPresent) != 1 || r.FilesPresent[0] != "hot-memory" {
		t.Errorf("files_present = %v, want [hot-memory]", r.FilesPresent)
	}
	if r.OpenActionCount != 0 || r.CompletedActionCountSince != 0 {
		t.Errorf("counts should be zero with missing action-items: %+v", r)
	}
	if r.RecentObservations == nil {
		t.Errorf("recent_observations should be empty array, not null")
	}
}

func TestDomainSummaryRoleRequired(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domain_summary",
		Params: map[string]interface{}{"domain": "personal"},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected InvalidParams for missing role, got %+v", resp.Error)
	}
}

// Confirms a YAML override at write time is reflected in summary output —
// also makes sure manifest hot-reload pathway still works under summary.
func TestDomainSummaryHotReloadsManifest(t *testing.T) {
	ts := newTestServer(t)
	// Replace domains.yml with a new label.
	newYAML := `version: 1
domains:
  - id: personal
    path: personal
    label: Personal Renamed
    files: [hot-memory, action-items, observations, entities]
`
	manifest := filepath.Join(ts.memoryRoot, "domains.yml")
	// Push mtime forward to guarantee detection.
	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(manifest, []byte(newYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(manifest, future, future); err != nil {
		t.Fatal(err)
	}
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domain_summary",
		Params: map[string]interface{}{"role": "siona", "domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("domain_summary: %v", resp.Error.Message)
	}
	var r summaryResult
	json.Unmarshal(resp.Result, &r)
	if r.Label != "Personal Renamed" {
		t.Errorf("expected hot-reloaded label, got %q", r.Label)
	}
}
