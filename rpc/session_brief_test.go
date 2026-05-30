package rpc_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bketelsen/cogmemory/rpc"
)

func TestSessionBriefReturnsEnvelope(t *testing.T) {
	ts := newTestServer(t)

	// Seed hot-memory + patterns + action-items in two domains.
	for _, w := range []struct {
		path, body string
	}{
		{"hot-memory.md", "# Hot\nstrategic state\n"},
		{"cog-meta/patterns.md", "# Patterns\nrule one\n"},
		{"projects/dakota/action-items.md", "- [ ] dakota task | pri:high\n- [ ] another | pri:medium\n"},
		{"personal/action-items.md", "- [ ] private | pri:low\n"},
		{"work/microsoft/action-items.md", "- [ ] msft task | pri:high\n"},
	} {
		resp := call(t, ts.socketPath, rpcRequest{
			JSONRPC: "2.0", ID: 1, Method: "write",
			Params: map[string]interface{}{
				"role": "siona", "path": w.path, "content": w.body,
			},
		})
		if resp.Error != nil {
			t.Fatalf("seed write %s: %v", w.path, resp.Error.Message)
		}
	}

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 10, Method: "session_brief",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("session_brief: %v", resp.Error.Message)
	}

	var result struct {
		HotMemory    string                 `json:"hot_memory"`
		Patterns     string                 `json:"patterns"`
		Domains      []struct {
			ID       string   `json:"id"`
			Label    string   `json:"label"`
			Triggers []string `json:"triggers"`
		} `json:"domains"`
		ActionCounts        map[string]interface{} `json:"action_counts"`
		ControllerLastError interface{}            `json:"controller_last_error"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, string(resp.Result))
	}

	if !strings.Contains(result.HotMemory, "strategic state") {
		t.Errorf("hot_memory missing seed content: %q", result.HotMemory)
	}
	if !strings.Contains(result.Patterns, "rule one") {
		t.Errorf("patterns missing seed content: %q", result.Patterns)
	}
	ids := map[string]bool{}
	for _, d := range result.Domains {
		ids[d.ID] = true
	}
	for _, want := range []string{"dakota", "personal", "work"} {
		if !ids[want] {
			t.Errorf("domain %q missing from envelope; got %v", want, ids)
		}
	}

	if v, ok := result.ActionCounts["dakota"]; !ok || toInt(v) != 2 {
		t.Errorf("action_counts[dakota] = %v, want 2", v)
	}
	if v, ok := result.ActionCounts["personal"]; !ok || toInt(v) != 1 {
		t.Errorf("action_counts[personal] = %v, want 1", v)
	}
	if v, ok := result.ActionCounts["work"]; !ok || toInt(v) != 1 {
		t.Errorf("action_counts[work] = %v, want 1", v)
	}
	pri, ok := result.ActionCounts["_pri_high_anywhere"].(bool)
	if !ok || !pri {
		t.Errorf("_pri_high_anywhere = %v, want true", result.ActionCounts["_pri_high_anywhere"])
	}
	if result.ControllerLastError != nil {
		t.Errorf("controller_last_error = %v, want nil", result.ControllerLastError)
	}
}

// RBAC: a role with no read on personal/ should not see personal in domains
// or action_counts. Hot-memory + patterns are owner-canonical, returned
// regardless.
func TestSessionBriefRBACFiltersDomainsAndCounts(t *testing.T) {
	ts := newTestServer(t)

	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role": "siona", "path": "hot-memory.md", "content": "hot\n",
		},
	})
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "write",
		Params: map[string]interface{}{
			"role": "siona", "path": "projects/dakota/action-items.md",
			"content": "- [ ] dakota | pri:high\n",
		},
	})
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "write",
		Params: map[string]interface{}{
			"role": "siona", "path": "personal/action-items.md",
			"content": "- [ ] private | pri:high\n",
		},
	})

	// project-reader sees only projects/**
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 10, Method: "session_brief",
		Params: map[string]interface{}{"role": "project-reader"},
	})
	if resp.Error != nil {
		t.Fatalf("session_brief: %v", resp.Error.Message)
	}

	var result struct {
		HotMemory    string                 `json:"hot_memory"`
		Domains      []struct{ ID string }  `json:"domains"`
		ActionCounts map[string]interface{} `json:"action_counts"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.HotMemory == "" {
		t.Error("hot_memory should be returned even for limited roles (owner-canonical)")
	}
	ids := map[string]bool{}
	for _, d := range result.Domains {
		ids[d.ID] = true
	}
	if !ids["dakota"] {
		t.Errorf("expected dakota in visible domains, got %v", ids)
	}
	if ids["personal"] || ids["work"] {
		t.Errorf("project-reader should not see personal/work; got %v", ids)
	}
	if _, has := result.ActionCounts["dakota"]; !has {
		t.Errorf("action_counts should include dakota, got %v", result.ActionCounts)
	}
	if _, has := result.ActionCounts["personal"]; has {
		t.Errorf("action_counts should NOT include personal for project-reader, got %v", result.ActionCounts)
	}
}

func TestSessionBriefMissingRoleErrors(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "session_brief",
		Params: map[string]interface{}{"role": ""},
	})
	if resp.Error == nil {
		t.Fatal("expected error for empty role")
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, rpc.CodeInvalidParams)
	}
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return -1
}
