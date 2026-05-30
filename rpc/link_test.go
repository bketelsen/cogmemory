package rpc_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// writeFile is a small helper that writes a memory file via the RPC server
// using the all-powerful "siona" role so seeding is independent of the role
// under test.
func writeFile(t *testing.T, ts *testServer, id int, path, content string) {
	t.Helper()
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: id, Method: "write",
		Params: map[string]interface{}{"role": "siona", "path": path, "content": content},
	})
	if resp.Error != nil {
		t.Fatalf("seed write %q: %v", path, resp.Error.Message)
	}
}

func TestLinkIndexCompute(t *testing.T) {
	ts := newTestServer(t)

	writeFile(t, ts, 1, filepath.ToSlash("personal/observations.md"),
		"see [[personal/entities#Jane]] and [[work/microsoft/hot-memory]]\n"+
			"also [[personal/entities#Jane]] again — should dedupe\n")
	writeFile(t, ts, 2, filepath.ToSlash("work/microsoft/observations.md"),
		"meeting with [[personal/entities#Jane]] today\n")
	writeFile(t, ts, 3, filepath.ToSlash("personal/entities.md"), "# Entities\n")

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 10, Method: "link_index_compute",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("link_index_compute: %v", resp.Error.Message)
	}

	var result struct {
		Links []struct {
			Target  string   `json:"target"`
			Sources []string `json:"sources"`
		} `json:"links"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := map[string][]string{}
	for _, e := range result.Links {
		got[e.Target] = e.Sources
	}

	wantTarget := "personal/entities"
	if sources, ok := got[wantTarget]; !ok {
		t.Fatalf("missing target %q in %v", wantTarget, got)
	} else {
		want := []string{"personal/observations", "work/microsoft/observations"}
		if !equalStrings(sources, want) {
			t.Errorf("target %q sources = %v, want %v", wantTarget, sources, want)
		}
	}

	if sources, ok := got["work/microsoft/hot-memory"]; !ok {
		t.Errorf("missing target work/microsoft/hot-memory")
	} else if len(sources) != 1 || sources[0] != "personal/observations" {
		t.Errorf("hot-memory sources = %v, want [personal/observations]", sources)
	}
}

func TestLinkIndexComputeRBACFilters(t *testing.T) {
	ts := newTestServer(t)
	// projects/** is readable for coder; everything else is read-only via the
	// catch-all read:true rule. project-reader can read projects/** but
	// NOTHING outside it.
	writeFile(t, ts, 1, filepath.ToSlash("personal/observations.md"),
		"link to [[projects/dakota/hot-memory]] from personal\n")
	writeFile(t, ts, 2, filepath.ToSlash("projects/dakota/observations.md"),
		"link to [[projects/dakota/hot-memory]] from project\n")

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "link_index_compute",
		Params: map[string]interface{}{"role": "project-reader"},
	})
	if resp.Error != nil {
		t.Fatalf("link_index_compute: %v", resp.Error.Message)
	}
	var result struct {
		Links []struct {
			Target  string   `json:"target"`
			Sources []string `json:"sources"`
		} `json:"links"`
	}
	json.Unmarshal(resp.Result, &result)
	for _, e := range result.Links {
		for _, src := range e.Sources {
			if !pathHasPrefix(src, "projects/") {
				t.Errorf("project-reader saw source %q (target %q) outside projects/", src, e.Target)
			}
		}
	}
	// And we should still see the project source.
	found := false
	for _, e := range result.Links {
		if e.Target == "projects/dakota/hot-memory" {
			for _, src := range e.Sources {
				if src == "projects/dakota/observations" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected projects/dakota/observations to survive RBAC filter")
	}
}

func TestLinkIndexComputeMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "link_index_compute",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("want -32602 invalid params, got %+v", resp.Error)
	}
}

func TestLinkAudit(t *testing.T) {
	ts := newTestServer(t)
	writeFile(t, ts, 1, filepath.ToSlash("personal/entities.md"),
		"# Entities\n\n### Jane Smith (CTO)\nRole: CTO at Acme\n\n### Bob\nstatus: active\n")
	// File mentions Jane unlinked; mentions Bob with a link already.
	writeFile(t, ts, 2, filepath.ToSlash("personal/observations.md"),
		"- 2026-05-30 [meeting]: spoke with Jane Smith about onboarding\n"+
			"- 2026-05-30 [meeting]: also met [[personal/entities#Bob]] (already linked)\n"+
			"- 2026-05-30 [note]: Jane Smith follow-up tomorrow\n")
	// Entity's own home file should NOT generate a candidate for itself.
	writeFile(t, ts, 3, filepath.ToSlash("work/microsoft/observations.md"),
		"Bob is rolling out the new dashboard\n")

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 10, Method: "link_audit",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("link_audit: %v", resp.Error.Message)
	}

	var result struct {
		Candidates []struct {
			SourcePath string `json:"source_path"`
			Line       int    `json:"line"`
			EntityName string `json:"entity_name"`
			TargetLink string `json:"target_link"`
			Context    string `json:"context"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Expect:
	//   personal/observations line 1: Jane Smith -> personal/entities#Jane Smith
	//   personal/observations line 3: Jane Smith
	//   work/microsoft/observations line 1: Bob
	// Should NOT include:
	//   personal/observations line 2 (already-linked Bob)
	//   personal/entities itself
	var (
		sawJane1, sawJane3, sawBobWork bool
	)
	for _, c := range result.Candidates {
		if c.SourcePath == "personal/entities.md" {
			t.Errorf("entity home file leaked as candidate: %+v", c)
		}
		if c.SourcePath == "personal/observations.md" && c.EntityName == "Bob" {
			t.Errorf("already-linked Bob surfaced as candidate: %+v", c)
		}
		if c.SourcePath == "personal/observations.md" && c.EntityName == "Jane Smith" && c.Line == 1 {
			sawJane1 = true
			if c.TargetLink != "personal/entities#Jane Smith" {
				t.Errorf("target_link = %q, want %q", c.TargetLink, "personal/entities#Jane Smith")
			}
		}
		if c.SourcePath == "personal/observations.md" && c.EntityName == "Jane Smith" && c.Line == 3 {
			sawJane3 = true
		}
		if c.SourcePath == "work/microsoft/observations.md" && c.EntityName == "Bob" {
			sawBobWork = true
		}
	}
	if !sawJane1 || !sawJane3 {
		t.Errorf("expected both Jane Smith candidates in personal/observations (line 1 & 3); saw1=%v saw3=%v", sawJane1, sawJane3)
	}
	if !sawBobWork {
		t.Errorf("expected Bob candidate in work/microsoft/observations")
	}
}

func TestLinkAuditWholeWordBoundary(t *testing.T) {
	ts := newTestServer(t)
	writeFile(t, ts, 1, filepath.ToSlash("personal/entities.md"),
		"### Bob\nstatus: active\n")
	writeFile(t, ts, 2, filepath.ToSlash("personal/observations.md"),
		"Bobcat is not Bob\n"+ // Bobcat MUST NOT match
			"Bob's lunch\n")

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "link_audit",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("link_audit: %v", resp.Error.Message)
	}
	var result struct {
		Candidates []struct {
			SourcePath string `json:"source_path"`
			Line       int    `json:"line"`
			EntityName string `json:"entity_name"`
		} `json:"candidates"`
	}
	json.Unmarshal(resp.Result, &result)
	for _, c := range result.Candidates {
		if c.Line == 1 {
			// Line 1 contains "Bobcat is not Bob" — the second "Bob" IS a
			// whole-word match (boundary on each side), so a hit here is fine.
			// What we MUST NOT see is two hits on line 1 (one for Bobcat).
		}
	}
	// Specifically check we matched line 1's standalone Bob, not Bobcat.
	hits := 0
	for _, c := range result.Candidates {
		if c.EntityName == "Bob" {
			hits++
		}
	}
	// Two lines, each mentions Bob exactly once outside of any link.
	if hits != 2 {
		t.Errorf("expected 2 Bob candidates, got %d: %+v", hits, result.Candidates)
	}
}

func TestLinkAuditMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "link_audit",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("want -32602 invalid params, got %+v", resp.Error)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pathHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
