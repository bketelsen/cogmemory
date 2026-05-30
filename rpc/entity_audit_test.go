package rpc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEntityAuditAllDomains(t *testing.T) {
	ts := newTestServer(t)

	// work/microsoft/entities.md — clean compact entity (no violations).
	workPath := filepath.Join(ts.memoryRoot, "work", "microsoft", "entities.md")
	if err := os.MkdirAll(filepath.Dir(workPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workPath, []byte("### Microsoft\nRole: employer\nstatus: active | last: 2026-05-27\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// personal/entities.md — too long, with detail link, missing nothing.
	persPath := filepath.Join(ts.memoryRoot, "personal", "entities.md")
	if err := os.MkdirAll(filepath.Dir(persPath), 0o755); err != nil {
		t.Fatal(err)
	}
	personalBody := `### Friend
Line one
Line two
Line three
Line four → [[wiki:pages/people/friend]]
status: active | last: 2026-05-27
`
	if err := os.WriteFile(persPath, []byte(personalBody), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "entity_audit",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("entity_audit error: %v", resp.Error.Message)
	}
	var result struct {
		FormatViolations []struct {
			Path          string `json:"path"`
			Name          string `json:"name"`
			Lines         int    `json:"lines"`
			Issue         string `json:"issue"`
			HasDetailFile bool   `json:"has_detail_file"`
		} `json:"format_violations"`
		GlacierCandidates  []map[string]interface{} `json:"glacier_candidates"`
		MissingMetadata    []map[string]interface{} `json:"missing_metadata"`
		TemporalViolations []map[string]interface{} `json:"temporal_violations"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.FormatViolations) != 1 {
		t.Fatalf("want 1 format violation, got %+v", result.FormatViolations)
	}
	v := result.FormatViolations[0]
	if v.Name != "Friend" || !v.HasDetailFile || v.Issue != "exceeds_3_line_compact" {
		t.Errorf("unexpected violation: %+v", v)
	}
}

func TestEntityAuditScopedToDomain(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"work/microsoft", "personal"} {
		dir := filepath.Join(ts.memoryRoot, p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Each entity missing both fields → flagged if scanned.
		if err := os.WriteFile(filepath.Join(dir, "entities.md"), []byte("### X\nplain prose\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "entity_audit",
		Params: map[string]interface{}{"role": "siona", "domain": "work"},
	})
	if resp.Error != nil {
		t.Fatalf("entity_audit error: %v", resp.Error.Message)
	}
	var result struct {
		MissingMetadata []map[string]interface{} `json:"missing_metadata"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.MissingMetadata) != 1 {
		t.Fatalf("expected 1 entry (work only), got %+v", result.MissingMetadata)
	}
	if got := result.MissingMetadata[0]["path"]; got != "work/microsoft/entities.md" {
		t.Errorf("wrong path: %v", got)
	}
}

func TestEntityAuditRequiresRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "entity_audit",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("expected error when role missing")
	}
}

func TestEntityAuditUnknownDomain(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "entity_audit",
		Params: map[string]interface{}{"role": "siona", "domain": "nope"},
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown domain")
	}
}
