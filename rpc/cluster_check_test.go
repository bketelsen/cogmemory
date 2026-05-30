package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClusterCheckMethodReturnsTagClusters(t *testing.T) {
	ts := newTestServer(t)
	// Drop observations into the "personal" domain seeded by defaultDomainsYAML.
	abs := filepath.Join(ts.memoryRoot, "personal", "observations.md")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"- 2126-05-12 [health, sleep]: 4 hours",
		"- 2126-05-15 [health]: low energy",
		"- 2126-05-20 [health]: skipped breakfast",
		"- 2126-05-29 [health]: better sleep",
	}, "\n") + "\n"
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "cluster_check",
		Params: map[string]interface{}{
			"role":             "siona",
			"since":            "2126-01-01",
			"min_cluster_size": 3,
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var out struct {
		ByTag []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"by_tag"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.ByTag) == 0 || out.ByTag[0].Tag != "health" || out.ByTag[0].Count != 4 {
		t.Fatalf("expected health=4 first cluster, got %+v", out.ByTag)
	}
}

func TestClusterCheckMethodRequiresRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "cluster_check",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing role")
	}
}

func TestClusterCheckMethodInvalidSince(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "cluster_check",
		Params: map[string]interface{}{"role": "siona", "since": "yesterday"},
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid since")
	}
}

func TestClusterCheckMethodRBACFiltersTargets(t *testing.T) {
	ts := newTestServer(t)
	// Put observations in personal (restricted from project-reader) AND
	// projects/dakota (allowed).
	for _, p := range []string{
		"personal/observations.md",
		"projects/dakota/observations.md",
	} {
		abs := filepath.Join(ts.memoryRoot, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		content := strings.Join([]string{
			"- 2126-05-10 [secret]: x",
			"- 2126-05-12 [secret]: y",
			"- 2126-05-14 [secret]: z",
		}, "\n") + "\n"
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// project-reader: projects/** read, **=deny. Should ONLY see projects/dakota
	// targets, never personal.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "cluster_check",
		Params: map[string]interface{}{
			"role":             "project-reader",
			"since":            "2126-01-01",
			"min_cluster_size": 3,
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var out struct {
		ByTag []struct {
			Tag     string   `json:"tag"`
			Count   int      `json:"count"`
			Domains []string `json:"domains"`
		} `json:"by_tag"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.ByTag) != 1 || out.ByTag[0].Count != 3 {
		t.Fatalf("expected exactly 3 entries (projects only), got %+v", out.ByTag)
	}
	for _, d := range out.ByTag[0].Domains {
		if d == "personal" {
			t.Fatalf("RBAC leak: personal observations included for project-reader")
		}
	}
}
