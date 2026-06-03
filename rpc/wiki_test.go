package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWikiIndexComputeMethod(t *testing.T) {
	ts := newTestServer(t)

	honchoDir := filepath.Join(ts.memoryRoot, "wiki", "research", "honcho")
	if err := os.MkdirAll(honchoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	honcho := `---
title: Honcho
summary: Eval as memory layer for Hermes agent; verdict deferred.
updated: 2026-05-19
entity_type: research
status: active
tags: [memory, self-hosting, agents]
related: [wiki/topics/semantic-memory-search, wiki/tools/monet]
---
content
`
	if err := os.WriteFile(filepath.Join(honchoDir, "index.md"), []byte(honcho), 0o644); err != nil {
		t.Fatal(err)
	}

	// A page with no frontmatter — must still appear with only Path set.
	noFM := filepath.Join(ts.memoryRoot, "wiki", "topics")
	if err := os.MkdirAll(noFM, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noFM, "orphan.md"), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Excluded files: generated catalog + registry. Must NOT appear.
	if err := os.WriteFile(filepath.Join(ts.memoryRoot, "wiki", "index.md"), []byte("---\ntitle: Catalog\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaDir := filepath.Join(ts.memoryRoot, "wiki", "_meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "registry.md"), []byte("---\ntitle: Registry\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "wiki_index_compute",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("wiki_index_compute error: %v", resp.Error.Message)
	}
	var result struct {
		Entries []struct {
			Path     string   `json:"path"`
			Category string   `json:"category"`
			Title    string   `json:"title"`
			Status   string   `json:"status"`
			Tags     []string `json:"tags"`
			Summary  string   `json:"summary"`
			Updated  string   `json:"updated"`
			Related  []string `json:"related"`
		} `json:"entries"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Exactly the two content pages — index.md and _meta/* excluded.
	if result.Count != 2 || len(result.Entries) != 2 {
		t.Fatalf("got %d entries (count=%d): %+v", len(result.Entries), result.Count, result.Entries)
	}
	// Sorted ascending by path: research/honcho/index.md < topics/orphan.md.
	e0 := result.Entries[0]
	if e0.Path != "wiki/research/honcho/index.md" ||
		e0.Category != "research" ||
		e0.Title != "Honcho" ||
		e0.Status != "active" ||
		e0.Updated != "2026-05-19" ||
		len(e0.Tags) != 3 || e0.Tags[0] != "memory" ||
		len(e0.Related) != 2 || e0.Related[0] != "wiki/topics/semantic-memory-search" {
		t.Errorf("entry[0] = %+v", e0)
	}
	e1 := result.Entries[1]
	if e1.Path != "wiki/topics/orphan.md" {
		t.Errorf("entry[1] path = %q, want wiki/topics/orphan.md", e1.Path)
	}
	if e1.Category != "" || e1.Title != "" {
		t.Errorf("orphan should have empty metadata: %+v", e1)
	}
	if e1.Tags == nil {
		t.Errorf("orphan Tags should be non-nil empty slice")
	}
	// Ensure excluded paths are truly absent.
	for _, e := range result.Entries {
		if e.Path == "wiki/index.md" || e.Path == "wiki/_meta/registry.md" {
			t.Errorf("excluded path leaked into catalog: %q", e.Path)
		}
	}
}

func TestWikiIndexComputeRBACFilters(t *testing.T) {
	ts := newTestServer(t)

	pdir := filepath.Join(ts.memoryRoot, "wiki", "projects")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "a.md"), []byte("---\ntitle: A\nentity_type: projects\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	persDir := filepath.Join(ts.memoryRoot, "wiki", "personal")
	if err := os.MkdirAll(persDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(persDir, "p.md"), []byte("---\ntitle: P\nentity_type: people\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// project-reader pattern is projects/** — does NOT match wiki/projects/**,
	// so zero entries are visible (pattern is path-prefix-literal, not semantic).
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "wiki_index_compute",
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
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Count != 0 {
		t.Fatalf("project-reader should see 0 wiki entries, got %d: %+v", result.Count, result.Entries)
	}

	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "wiki_index_compute",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("err: %v", resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("siona should see 2 entries, got %d", result.Count)
	}
}

func TestWikiIndexComputeMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "wiki_index_compute",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing role")
	}
}
