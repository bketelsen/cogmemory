package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGlacierIndexComputeMethod(t *testing.T) {
	ts := newTestServer(t)

	gdir := filepath.Join(ts.memoryRoot, "glacier", "projects")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
type: action-items-done
domain: projects
date_range: 2026-05-18 to 2026-05-22
entries: 14
summary: Sprint items
tags: [housekeeping]
---
content
`
	if err := os.WriteFile(filepath.Join(gdir, "a.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "b.md"), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "glacier_index_compute",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("glacier_index_compute error: %v", resp.Error.Message)
	}
	var result struct {
		Entries []struct {
			Path      string   `json:"path"`
			Domain    string   `json:"domain"`
			Type      string   `json:"type"`
			Tags      []string `json:"tags"`
			DateRange string   `json:"date_range"`
			Entries   int      `json:"entries"`
			Summary   string   `json:"summary"`
		} `json:"entries"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Count != 2 || len(result.Entries) != 2 {
		t.Fatalf("got %d entries (count=%d): %+v", len(result.Entries), result.Count, result.Entries)
	}
	if result.Entries[0].Path != "glacier/projects/a.md" ||
		result.Entries[0].Type != "action-items-done" ||
		result.Entries[0].Entries != 14 ||
		len(result.Entries[0].Tags) != 1 || result.Entries[0].Tags[0] != "housekeeping" {
		t.Errorf("entry[0] = %+v", result.Entries[0])
	}
}

func TestGlacierIndexComputeRBACFilters(t *testing.T) {
	ts := newTestServer(t)
	gdir := filepath.Join(ts.memoryRoot, "glacier", "projects")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "p.md"), []byte("---\ndomain: projects\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdir := filepath.Join(ts.memoryRoot, "glacier", "personal")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "priv.md"), []byte("---\ndomain: personal\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// project-reader pattern is projects/** — does NOT match glacier/projects/**,
	// so zero entries are visible (pattern is path-prefix-literal, not semantic).
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "glacier_index_compute",
		Params: map[string]interface{}{"role": "project-reader"},
	})
	if resp.Error != nil {
		t.Fatalf("err: %v", resp.Error.Message)
	}
	var result struct {
		Entries []struct {
			Path string `json:"path"`
		} `json:"entries"`
		Count int `json:"count"`
	}
	json.Unmarshal(resp.Result, &result)
	if result.Count != 0 {
		t.Fatalf("project-reader should see 0 glacier entries (none match projects/**), got %d: %+v",
			result.Count, result.Entries)
	}

	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "glacier_index_compute",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("err: %v", resp.Error.Message)
	}
	json.Unmarshal(resp.Result, &result)
	if result.Count != 2 {
		t.Fatalf("siona should see 2 entries, got %d", result.Count)
	}
}

func TestGlacierIndexComputeMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "glacier_index_compute",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing role")
	}
}
