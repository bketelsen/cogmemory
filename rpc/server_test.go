package rpc_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bketelsen/cogmemory/config"
	"github.com/bketelsen/cogmemory/domain"
	"github.com/bketelsen/cogmemory/rbac"
	"github.com/bketelsen/cogmemory/rpc"
	"github.com/bketelsen/cogmemory/store"
)

type testServer struct {
	srv        *rpc.Server
	socketPath string
	memoryRoot string
	ln         net.Listener
	done       chan struct{}
}

const defaultDomainsYAML = `version: 1
domains:
  - id: dakota
    path: projects/dakota
    label: Dakota
    triggers: [dakota]
    files: [hot-memory, action-items, observations, entities]
  - id: personal
    path: personal
    label: Personal
    triggers: [personal]
    files: [hot-memory, action-items, observations, entities]
  - id: work
    path: work/microsoft
    label: Microsoft
    triggers: [work, msft]
    files: [hot-memory, action-items, observations, entities]
`

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	memoryRoot := filepath.Join(dir, "memory")
	s, err := store.New(memoryRoot)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.RBACConfig{
		Roles: map[string][]config.Rule{
			"siona": {
				{Pattern: "**", Read: true, Write: true},
			},
			"researcher": {
				{Pattern: "**", Read: true, Write: false},
			},
			"coder": {
				{Pattern: "projects/**", Read: true, Write: true},
				{Pattern: "cog-meta/self-observations.md", Read: false, Write: false},
				{Pattern: "**", Read: true, Write: false},
			},
			"project-reader": {
				{Pattern: "projects/**", Read: true, Write: false},
				{Pattern: "**", Read: false, Write: false},
			},
		},
	}
	r := rbac.New(cfg)
	// Seed a default domains.yml covering the action-items fixtures used
	// across these tests. Individual tests may overwrite it.
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "domains.yml"), []byte(defaultDomainsYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl, err := domain.New(memoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	srv := rpc.New(s, r, ctrl)

	socketPath := filepath.Join(dir, "test.sock")
	ln, err := rpc.Listen(socketPath)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ln)
	}()

	t.Cleanup(func() {
		ln.Close()
		<-done
		srv.Wait()
		os.Remove(socketPath)
	})

	return &testServer{srv: srv, socketPath: socketPath, memoryRoot: memoryRoot, ln: ln, done: done}
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func call(t *testing.T, socketPath string, req rpcRequest) rpcResponse {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response from server")
	}

	var resp rpcResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp
}

// --- Tests ---

func TestReadMethod(t *testing.T) {
	ts := newTestServer(t)

	// First write a file
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "write",
		Params:  map[string]interface{}{"role": "siona", "path": "notes.md", "content": "hello\n"},
	})

	// Now read it
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "read",
		Params:  map[string]interface{}{"role": "siona", "path": "notes.md"},
	})
	if resp.Error != nil {
		t.Fatalf("read error: %v", resp.Error.Message)
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)
	if result["content"] != "hello\n" {
		t.Errorf("content = %v, want %q", result["content"], "hello\n")
	}
}

func TestWriteRBACDenied(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "write",
		Params:  map[string]interface{}{"role": "researcher", "path": "notes.md", "content": "should fail"},
	})

	if resp.Error == nil {
		t.Fatal("expected RBAC error, got success")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestPatchMethod(t *testing.T) {
	ts := newTestServer(t)

	// Write the file first
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{"role": "siona", "path": "doc.md", "content": "hello world\n"},
	})

	// Patch it
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "patch",
		Params: map[string]interface{}{
			"role":     "siona",
			"path":     "doc.md",
			"old_text": "hello",
			"new_text": "goodbye",
		},
	})
	if resp.Error != nil {
		t.Fatalf("patch error: %v", resp.Error.Message)
	}

	// Verify
	readResp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "read",
		Params: map[string]interface{}{"role": "siona", "path": "doc.md"},
	})
	var result map[string]interface{}
	json.Unmarshal(readResp.Result, &result)
	if result["content"] != "goodbye world\n" {
		t.Errorf("after patch, content = %v", result["content"])
	}
}

func TestAppendObsEnforcementViaRPC(t *testing.T) {
	ts := newTestServer(t)

	// Valid observation
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "append",
		Params: map[string]interface{}{
			"role": "siona",
			"path": "personal/observations.md",
			"text": "- 2025-01-01 [insight]: test observation\n",
		},
	})
	if resp.Error != nil {
		t.Fatalf("valid obs rejected: %v", resp.Error.Message)
	}

	// Invalid observation
	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "append",
		Params: map[string]interface{}{
			"role": "siona",
			"path": "personal/observations.md",
			"text": "not an observation line\n",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid observation format")
	}
	if !strings.Contains(resp.Error.Message, "observation format") {
		t.Errorf("error should mention observation format, got: %s", resp.Error.Message)
	}
}

// TestAppendSectionViaRPC locks in the JSON-RPC wire contract for the
// optional `section` param. Regression guard: a stale daemon binary that
// predated the section field silently dropped it during JSON unmarshal and
// landed every append at EOF — this test fails fast if the wire schema or
// dispatcher ever loses the param again.
func TestAppendSectionViaRPC(t *testing.T) {
	ts := newTestServer(t)

	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "action-items.md",
			"content": "# x\n\n## Open\n\n## Completed\n",
		},
	})

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "append",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "action-items.md",
			"text":    "- [ ] under Open\n",
			"section": "## Open",
		},
	})
	if resp.Error != nil {
		t.Fatalf("section append rejected: %v", resp.Error.Message)
	}

	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "read",
		Params: map[string]interface{}{"role": "siona", "path": "action-items.md"},
	})
	if resp.Error != nil {
		t.Fatalf("read failed: %v", resp.Error.Message)
	}
	var got struct {
		Content string `json:"content"`
	}
	b, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(b, &got)

	openIdx := strings.Index(got.Content, "## Open")
	completedIdx := strings.Index(got.Content, "## Completed")
	itemIdx := strings.Index(got.Content, "- [ ] under Open")
	if openIdx < 0 || completedIdx < 0 || itemIdx < 0 {
		t.Fatalf("missing expected markers in:\n%s", got.Content)
	}
	if !(openIdx < itemIdx && itemIdx < completedIdx) {
		t.Fatalf("section param ignored — item landed outside ## Open. Content:\n%s", got.Content)
	}

	// Bare title (no leading '#') must also work.
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 4, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "bare.md",
			"content": "## Open\n\n## Completed\n",
		},
	})
	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 5, Method: "append",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "bare.md",
			"text":    "- [ ] bare\n",
			"section": "Open",
		},
	})
	if resp.Error != nil {
		t.Fatalf("bare-title section append rejected: %v", resp.Error.Message)
	}
	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 6, Method: "read",
		Params: map[string]interface{}{"role": "siona", "path": "bare.md"},
	})
	b, _ = json.Marshal(resp.Result)
	_ = json.Unmarshal(b, &got)
	if i, c, x := strings.Index(got.Content, "## Open"), strings.Index(got.Content, "## Completed"), strings.Index(got.Content, "- [ ] bare"); !(i < x && x < c) {
		t.Fatalf("bare-title section param ignored. Content:\n%s", got.Content)
	}
}

func TestOutlineMethod(t *testing.T) {
	ts := newTestServer(t)
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role": "siona",
			"path": "doc.md",
			"content": strings.Join([]string{
				"# ignored",
				"## First",
				"text",
				"### Nested",
				"## Second",
			}, "\n"),
		},
	})

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "outline",
		Params: map[string]interface{}{"role": "siona", "path": "doc.md"},
	})
	if resp.Error != nil {
		t.Fatalf("outline error: %v", resp.Error.Message)
	}

	var result struct {
		Entries []struct {
			Line  int    `json:"line"`
			Text  string `json:"text"`
			Level int    `json:"level"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal outline result: %v", err)
	}
	want := []struct {
		Line  int    `json:"line"`
		Text  string `json:"text"`
		Level int    `json:"level"`
	}{
		{Line: 2, Text: "First", Level: 2},
		{Line: 4, Text: "Nested", Level: 3},
		{Line: 5, Text: "Second", Level: 2},
	}
	if fmt.Sprint(result.Entries) != fmt.Sprint(want) {
		t.Fatalf("entries = %+v, want %+v", result.Entries, want)
	}
}

func TestMoveMethod(t *testing.T) {
	ts := newTestServer(t)
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{"role": "siona", "path": "old.md", "content": "content\n"},
	})

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "move",
		Params: map[string]interface{}{"role": "siona", "from": "old.md", "to": "archive/new.md"},
	})
	if resp.Error != nil {
		t.Fatalf("move error: %v", resp.Error.Message)
	}
	if _, err := os.Stat(filepath.Join(ts.memoryRoot, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("old path still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ts.memoryRoot, "archive/new.md")); err != nil {
		t.Fatalf("new path missing: %v", err)
	}
}

func TestMoveMethodChecksWriteAccessOnDestination(t *testing.T) {
	ts := newTestServer(t)
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{"role": "siona", "path": "notes.md", "content": "content\n"},
	})

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "move",
		Params: map[string]interface{}{"role": "coder", "from": "notes.md", "to": "private/notes.md"},
	})
	if resp.Error == nil {
		t.Fatal("expected RBAC error")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestHealthMethod(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "health",
		Params:  map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("health error: %v", resp.Error.Message)
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)
	if result["ok"] != true {
		t.Errorf("health result = %v, want {ok: true}", result)
	}
}

func TestOpenActionsMethodReturnsReadableUncheckedItems(t *testing.T) {
	ts := newTestServer(t)

	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role": "siona",
			"path": "projects/dakota/action-items.md",
			"content": strings.Join([]string{
				"# Dakota Actions",
				"",
				"<!-- Format: - [ ] template | due:YYYY-MM-DD | pri:high -->",
				"- [ ] Ship open-actions RPC | due:2026-06-01 | pri:high | added:2026-05-30",
				"- [x] Closed task | pri:low",
			}, "\n"),
		},
	})
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/action-items.md",
			"content": "- [ ] Private task | pri:medium\n",
		},
	})

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "open_actions",
		Params:  map[string]interface{}{"role": "project-reader"},
	})
	if resp.Error != nil {
		t.Fatalf("open_actions error: %v", resp.Error.Message)
	}

	var result struct {
		Items []struct {
			Domain   string `json:"domain"`
			Path     string `json:"path"`
			Line     int    `json:"line"`
			Text     string `json:"text"`
			Raw      string `json:"raw"`
			Due      string `json:"due,omitempty"`
			Priority string `json:"priority,omitempty"`
			Added    string `json:"added,omitempty"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal open_actions result: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("open_actions returned %d items, want 1: %+v", len(result.Items), result.Items)
	}
	item := result.Items[0]
	if item.Domain != "dakota" ||
		item.Path != "projects/dakota/action-items.md" ||
		item.Line != 4 ||
		item.Text != "Ship open-actions RPC" ||
		item.Raw != "- [ ] Ship open-actions RPC | due:2026-06-01 | pri:high | added:2026-05-30" ||
		item.Due != "2026-06-01" ||
		item.Priority != "high" ||
		item.Added != "2026-05-30" {
		t.Fatalf("open_actions item = %+v", item)
	}
}

func TestOpenActionsMethodEmptyResultIsArray(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "open_actions",
		Params:  map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("open_actions error: %v", resp.Error.Message)
	}

	var result struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal open_actions result: %v", err)
	}
	if result.Items == nil {
		t.Fatal("items is nil, want empty JSON array")
	}
	if len(result.Items) != 0 {
		t.Fatalf("items length = %d, want 0", len(result.Items))
	}
}

// Broad role (siona) sees items from every action-items file regardless of
// domain. Without this, the only thing the RBAC filter is proven to do is
// hide personal/ from project-reader; the wide-permission path stays untested.
func TestOpenActionsMethodBroadRoleSeesAllItems(t *testing.T) {
	ts := newTestServer(t)

	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "projects/dakota/action-items.md",
			"content": "- [ ] project task | pri:high\n",
		},
	})
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/action-items.md",
			"content": "- [ ] personal task | pri:low\n",
		},
	})

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "open_actions",
		Params:  map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("open_actions error: %v", resp.Error.Message)
	}

	var result struct {
		Items []struct {
			Domain string `json:"domain"`
			Text   string `json:"text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal open_actions result: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("open_actions returned %d items, want 2: %+v", len(result.Items), result.Items)
	}
	domains := map[string]bool{}
	for _, it := range result.Items {
		domains[it.Domain] = true
	}
	if !domains["dakota"] || !domains["personal"] {
		t.Fatalf("expected both dakota and personal items; got domains %v", domains)
	}
}

// Reject malformed params loudly rather than silently treating role as "".
func TestOpenActionsMethodInvalidParams(t *testing.T) {
	ts := newTestServer(t)

	// Send raw bytes that aren't valid JSON for openActionsParams.
	conn, err := net.Dial("unix", ts.socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"open_actions","params":"not-an-object"}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, string(buf[:n]))
	}
	if resp.Error == nil {
		t.Fatalf("expected error for malformed params, got result: %s", string(resp.Result))
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Fatalf("error code = %d, want %d (%v)", resp.Error.Code, rpc.CodeInvalidParams, resp.Error.Message)
	}
}

// Empty role string must be rejected, not silently routed to rbac.Check("") and
// returning whatever that policy happens to allow.
func TestOpenActionsMethodMissingRole(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "open_actions",
		Params:  map[string]interface{}{"role": ""},
	})
	if resp.Error == nil {
		t.Fatalf("expected error for empty role, got result: %s", string(resp.Result))
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, rpc.CodeInvalidParams)
	}
}

func TestGitStatusMethodAllowsReadOnlyRole(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ts := newTestServer(t)
	// Start from an empty tree so the seeded domains.yml doesn't show up
	// as untracked in `git status --porcelain`.
	if err := os.Remove(filepath.Join(ts.memoryRoot, "domains.yml")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = ts.memoryRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, strings.TrimSpace(string(out)))
	}

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "git",
		Params:  map[string]interface{}{"role": "researcher", "op": "status"},
	})
	if resp.Error != nil {
		t.Fatalf("git status error: %v", resp.Error.Message)
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)
	if result["output"] != "" {
		t.Errorf("git status output = %v, want empty string", result["output"])
	}
}

func TestGitCommitMethodRequiresWriteAccess(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "git",
		Params: map[string]interface{}{
			"role":    "researcher",
			"op":      "commit",
			"message": "test commit",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected RBAC error for git commit")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestInvalidMethod(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "nonexistent_method",
		Params:  map[string]interface{}{"role": "siona"},
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestConcurrentRequests(t *testing.T) {
	ts := newTestServer(t)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan string, goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			resp := call(t, ts.socketPath, rpcRequest{
				JSONRPC: "2.0",
				ID:      n,
				Method:  "write",
				Params: map[string]interface{}{
					"role":    "siona",
					"path":    fmt.Sprintf("concurrent-%d.md", n),
					"content": fmt.Sprintf("writer %d\n", n),
				},
			})
			if resp.Error != nil {
				errs <- fmt.Sprintf("goroutine %d: %v", n, resp.Error.Message)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for msg := range errs {
		t.Error(msg)
	}
}

func TestListenRemovesStaleSocket(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	// Create a stale socket file
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	ln, err := rpc.Listen(socketPath)
	if err != nil {
		t.Fatalf("Listen with stale socket: %v", err)
	}
	ln.Close()
}

func TestDomainsListReturnsRegistry(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domains.list",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("domains.list error: %v", resp.Error.Message)
	}
	var result struct {
		Domains []struct {
			ID    string   `json:"id"`
			Path  string   `json:"path"`
			Files []string `json:"files"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Domains) != 3 {
		t.Fatalf("got %d domains, want 3: %+v", len(result.Domains), result.Domains)
	}
}

func TestDomainsListFiltersByRBAC(t *testing.T) {
	ts := newTestServer(t)
	// project-reader can only see projects/** — that's just the dakota domain.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domains.list",
		Params: map[string]interface{}{"role": "project-reader"},
	})
	if resp.Error != nil {
		t.Fatalf("domains.list error: %v", resp.Error.Message)
	}
	var result struct {
		Domains []struct {
			ID string `json:"id"`
		} `json:"domains"`
	}
	json.Unmarshal(resp.Result, &result)
	if len(result.Domains) != 1 || result.Domains[0].ID != "dakota" {
		t.Fatalf("RBAC filter wrong: %+v", result.Domains)
	}
}

func TestDomainsGetEnforcesRBAC(t *testing.T) {
	ts := newTestServer(t)
	// project-reader cannot read personal/.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domains.get",
		Params: map[string]interface{}{"role": "project-reader", "id": "personal"},
	})
	if resp.Error == nil || resp.Error.Code != rpc.CodeRBACDenied {
		t.Fatalf("expected RBACDenied, got %+v", resp.Error)
	}
	// But can read dakota.
	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "domains.get",
		Params: map[string]interface{}{"role": "project-reader", "id": "dakota"},
	})
	if resp.Error != nil {
		t.Fatalf("domains.get(dakota): %v", resp.Error.Message)
	}
}

func TestDomainsGetUnknownReturnsError(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "domains.get",
		Params: map[string]interface{}{"role": "siona", "id": "nope"},
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestOpenActionsDomainFilter(t *testing.T) {
	ts := newTestServer(t)
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "projects/dakota/action-items.md",
			"content": "- [ ] dakota task\n",
		},
	})
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "personal/action-items.md",
			"content": "- [ ] personal task\n",
		},
	})
	// Filter to dakota only — personal should not appear.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "open_actions",
		Params: map[string]interface{}{"role": "siona", "domain": "dakota"},
	})
	if resp.Error != nil {
		t.Fatalf("open_actions: %v", resp.Error.Message)
	}
	var result struct {
		Items []struct {
			Domain string `json:"domain"`
			Text   string `json:"text"`
		} `json:"items"`
	}
	json.Unmarshal(resp.Result, &result)
	if len(result.Items) != 1 || result.Items[0].Domain != "dakota" {
		t.Fatalf("filter did not isolate dakota: %+v", result.Items)
	}
}

func TestOpenActionsDomainFilterUnknownErrors(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "open_actions",
		Params: map[string]interface{}{"role": "siona", "domain": "ghost"},
	})
	if resp.Error == nil {
		t.Fatal("want error for unknown domain")
	}
}

// Confirms the refactored handler attributes domain from the controller
// (work/microsoft → "work") rather than the leaf basename ("microsoft").
func TestOpenActionsDomainComesFromController(t *testing.T) {
	ts := newTestServer(t)
	call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    "work/microsoft/action-items.md",
			"content": "- [ ] work task\n",
		},
	})
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "open_actions",
		Params: map[string]interface{}{"role": "siona", "domain": "work"},
	})
	if resp.Error != nil {
		t.Fatalf("open_actions: %v", resp.Error.Message)
	}
	var result struct {
		Items []struct {
			Domain string `json:"domain"`
		} `json:"items"`
	}
	json.Unmarshal(resp.Result, &result)
	if len(result.Items) != 1 || result.Items[0].Domain != "work" {
		t.Fatalf("want domain=work (from controller), got %+v", result.Items)
	}
}

// --- recent_observations ---

// seedObs writes a multi-line observations.md via the underlying store helper
// path (raw write of well-formed lines). The RPC `append` validates format,
// but tests bypass that by writing the full content with the regular `write`
// method since observations.md content here is intentionally well-formed.
func seedObs(t *testing.T, ts *testServer, path, body string) {
	t.Helper()
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 9000, Method: "write",
		Params: map[string]interface{}{
			"role":    "siona",
			"path":    path,
			"content": body,
		},
	})
	if resp.Error != nil {
		t.Fatalf("seed %s: %v", path, resp.Error.Message)
	}
}

type recentObsResult struct {
	Since   string `json:"since"`
	Entries []struct {
		Domain string   `json:"domain"`
		Path   string   `json:"path"`
		Line   int      `json:"line"`
		Date   string   `json:"date"`
		Tags   []string `json:"tags"`
		Text   string   `json:"text"`
	} `json:"entries"`
	ByDomain map[string]int `json:"by_domain"`
	ByTag    map[string]int `json:"by_tag"`
}

func TestRecentObservationsHappyPath(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", strings.Join([]string{
		"# Observations",
		"",
		"- 2026-05-28 [health, milestone]: walked 10k",
		"- 2026-05-29 [health]: slept 8h",
		"- 2026-05-20 [old]: pre-window entry",
		"",
	}, "\n"))
	seedObs(t, ts, "work/microsoft/observations.md", strings.Join([]string{
		"- 2026-05-29 [milestone]: shipped pr",
		"",
	}, "\n"))

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "2026-05-27"},
	})
	if resp.Error != nil {
		t.Fatalf("recent_observations: %v", resp.Error.Message)
	}
	var result recentObsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Since != "2026-05-27" {
		t.Fatalf("since = %q, want 2026-05-27", result.Since)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("entries = %d, want 3 (pre-window must be excluded). got: %+v", len(result.Entries), result.Entries)
	}
	// Newest first.
	if result.Entries[0].Date != "2026-05-29" {
		t.Fatalf("entries not sorted newest-first: %+v", result.Entries)
	}
	// by_domain aggregates: personal=2, work-sub=1 (canonical id from controller).
	if result.ByDomain["personal"] != 2 || result.ByDomain["work"] != 1 {
		t.Fatalf("by_domain wrong: %+v", result.ByDomain)
	}
	// by_tag aggregates over the filtered set: health=2, milestone=2.
	if result.ByTag["health"] != 2 || result.ByTag["milestone"] != 2 {
		t.Fatalf("by_tag wrong: %+v", result.ByTag)
	}
	// Tags parsed and trimmed.
	for _, e := range result.Entries {
		if e.Path == "personal/observations.md" && e.Date == "2026-05-28" {
			if len(e.Tags) != 2 || e.Tags[0] != "health" || e.Tags[1] != "milestone" {
				t.Fatalf("tag parse wrong: %+v", e.Tags)
			}
			if e.Text != "walked 10k" {
				t.Fatalf("text parse wrong: %q", e.Text)
			}
		}
	}
}

func TestRecentObservationsByTagFilter(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", strings.Join([]string{
		"- 2026-05-28 [health]: a",
		"- 2026-05-29 [milestone]: b",
		"",
	}, "\n"))
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "2026-05-01", "by_tag": "health"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc: %v", resp.Error.Message)
	}
	var result recentObsResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Entries) != 1 || result.Entries[0].Text != "a" {
		t.Fatalf("by_tag did not filter: %+v", result.Entries)
	}
	if result.ByTag["health"] != 1 || result.ByTag["milestone"] != 0 {
		t.Fatalf("aggregates reflect post-filter set: %+v", result.ByTag)
	}
}

func TestRecentObservationsByDomainFilter(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", "- 2026-05-29 [x]: p\n")
	seedObs(t, ts, "work/microsoft/observations.md", "- 2026-05-29 [x]: w\n")
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "2026-05-01", "by_domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc: %v", resp.Error.Message)
	}
	var result recentObsResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Entries) != 1 || result.Entries[0].Domain != "personal" {
		t.Fatalf("by_domain did not isolate: %+v", result.Entries)
	}
}

func TestRecentObservationsByDomainUnknownErrors(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "by_domain": "ghost"},
	})
	if resp.Error == nil {
		t.Fatal("want error for unknown domain id")
	}
}

// Characterization + regression guard for a real-world UX trap.
//
// The scoping param is `by_domain` and the window param is `since`. Sibling
// RPCs (open_actions, cluster_check, domain_summary, entity_audit) instead
// name their scope param `domain`, so callers reach for `domain:` here out of
// habit; `days:` is a likewise-plausible-but-wrong guess for the window.
// Because the handler uses a plain json.Unmarshal (no DisallowUnknownFields),
// both unknown keys are silently dropped: `days` falls back to the 7-day
// default window and `domain` leaves by_domain empty, so the call returns
// every domain. That silent degradation is the bug behind the 2026-06-11
// "domain filter is a no-op" report — the daemon filter itself works (see
// TestRecentObservationsByDomainFilter); the call simply used the wrong names.
//
// This test pins that behavior so a future silent rename/drop of the real
// `by_domain` param fails loudly here, and documents the correct contract
// right next to the trap. Mirrors TestAppendSectionViaRPC, which guards the
// same "JSON quietly dropped a field" failure mode for append's `section`.
func TestRecentObservationsWrongParamNamesAreSilentlyIgnored(t *testing.T) {
	ts := newTestServer(t)
	// Two domains, both with an entry inside the default 7-day window so the
	// dropped `days` value can't accidentally hide one of them.
	today := time.Now().UTC().Format("2006-01-02")
	seedObs(t, ts, "personal/observations.md", fmt.Sprintf("- %s [x]: p\n", today))
	seedObs(t, ts, "work/microsoft/observations.md", fmt.Sprintf("- %s [x]: w\n", today))

	// The names from the bug report: `domain` + `days`. Both are unknown
	// fields and are silently dropped -> no scope filter, default window ->
	// ALL domains come back. This is the "returns the same as no-domain"
	// symptom.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "days": 14, "domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc (wrong names): %v", resp.Error.Message)
	}
	var ignored recentObsResult
	json.Unmarshal(resp.Result, &ignored)
	if len(ignored.Entries) != 2 {
		t.Fatalf("wrong-name params should be ignored (all domains returned); got %d entries: %+v", len(ignored.Entries), ignored.Entries)
	}
	if ignored.ByDomain["personal"] != 1 || ignored.ByDomain["work"] != 1 {
		t.Fatalf("expected both domains present when `domain:` is ignored; by_domain=%+v", ignored.ByDomain)
	}

	// The correct param name actually scopes the scan.
	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "by_domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc (by_domain): %v", resp.Error.Message)
	}
	var scoped recentObsResult
	json.Unmarshal(resp.Result, &scoped)
	if len(scoped.Entries) != 1 || scoped.Entries[0].Domain != "personal" {
		t.Fatalf("by_domain should isolate personal; got %+v", scoped.Entries)
	}
}

func TestRecentObservationsInvalidSinceRejected(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "yesterday"},
	})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("want invalid-params error, got %+v", resp.Error)
	}
}

func TestRecentObservationsMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("want error for missing role")
	}
}

func TestRecentObservationsRBACFiltersUnreadablePaths(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", "- 2026-05-29 [x]: personal entry\n")
	seedObs(t, ts, "projects/dakota/observations.md", "- 2026-05-29 [x]: dakota entry\n")
	// project-reader only sees projects/**.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "project-reader", "since": "2026-05-01"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc: %v", resp.Error.Message)
	}
	var result recentObsResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Entries) != 1 || result.Entries[0].Domain != "dakota" {
		t.Fatalf("RBAC did not filter personal/: %+v", result.Entries)
	}
	if _, ok := result.ByDomain["personal"]; ok {
		t.Fatalf("by_domain leaked unreadable domain: %+v", result.ByDomain)
	}
}

func TestRecentObservationsEmptyResultShapesAreNotNull(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "2099-01-01"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc: %v", resp.Error.Message)
	}
	// Raw json must contain `"entries":[]`, `"by_domain":{}`, `"by_tag":{}`.
	raw := string(resp.Result)
	for _, want := range []string{`"entries":[]`, `"by_domain":{}`, `"by_tag":{}`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
}

func TestRecentObservationsDefaultSinceIs7Days(t *testing.T) {
	ts := newTestServer(t)
	// One entry from 30 days ago (out of default window), one from today.
	old := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	today := time.Now().UTC().Format("2006-01-02")
	seedObs(t, ts, "personal/observations.md", fmt.Sprintf("- %s [x]: old\n- %s [x]: new\n", old, today))
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc: %v", resp.Error.Message)
	}
	var result recentObsResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Entries) != 1 || result.Entries[0].Text != "new" {
		t.Fatalf("default window did not pick last 7 days: %+v", result.Entries)
	}
	if result.Since == "" {
		t.Fatal("since not echoed in response")
	}
}

func TestRecentObservationsSkipsFencedBlocks(t *testing.T) {
	ts := newTestServer(t)
	body := strings.Join([]string{
		"- 2026-05-29 [real]: actual entry",
		"```",
		"- 2026-05-29 [fake]: inside a code fence",
		"```",
		"",
	}, "\n")
	seedObs(t, ts, "personal/observations.md", body)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "2026-05-01"},
	})
	if resp.Error != nil {
		t.Fatalf("rpc: %v", resp.Error.Message)
	}
	var result recentObsResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Entries) != 1 || result.Entries[0].Tags[0] != "real" {
		t.Fatalf("fenced block leaked or real entry dropped: %+v", result.Entries)
	}
}

// --- scenario_check ---

func writeScenario(t *testing.T, root, rel, status, checkBy string) {
	t.Helper()
	body := "---\ntype: scenario\n"
	if status != "" {
		body += "status: " + status + "\n"
	}
	if checkBy != "" {
		body += "check-by: " + checkBy + "\n"
	}
	body += "---\n# scenario\n"
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type scenarioResult struct {
	Scenarios []struct {
		Path           string `json:"path"`
		CheckBy        string `json:"check_by"`
		Status         string `json:"status"`
		DaysUntilCheck int    `json:"days_until_check"`
	} `json:"scenarios"`
}

func TestScenarioCheckReturnsScheduledEntries(t *testing.T) {
	ts := newTestServer(t)
	writeScenario(t, ts.memoryRoot, "cog-meta/scenarios/overdue.md", "active", "2000-01-01")
	writeScenario(t, ts.memoryRoot, "cog-meta/scenarios/future.md", "active", "2099-12-31")
	writeScenario(t, ts.memoryRoot, "cog-meta/scenarios/resolved.md", "resolved", "2099-12-31")

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "scenario_check",
		Params:  map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("scenario_check error: %v", resp.Error.Message)
	}
	var got scenarioResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Scenarios) != 2 {
		t.Fatalf("got %d scenarios, want 2: %#v", len(got.Scenarios), got.Scenarios)
	}
	if got.Scenarios[0].Path != "cog-meta/scenarios/future.md" || got.Scenarios[0].Status != "active" || got.Scenarios[0].DaysUntilCheck <= 0 {
		t.Errorf("future entry = %#v", got.Scenarios[0])
	}
	if got.Scenarios[1].Path != "cog-meta/scenarios/overdue.md" || got.Scenarios[1].Status != "overdue" || got.Scenarios[1].DaysUntilCheck >= 0 {
		t.Errorf("overdue entry = %#v", got.Scenarios[1])
	}
}

func TestScenarioCheckEmptyResultIsArray(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "scenario_check",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("scenario_check error: %v", resp.Error.Message)
	}
	// JSON must serialize empty slice as [], not null.
	if !strings.Contains(string(resp.Result), `"scenarios":[]`) {
		t.Fatalf("expected scenarios:[], got %s", string(resp.Result))
	}
}

func TestScenarioCheckMissingRole(t *testing.T) {
	ts := newTestServer(t)
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "scenario_check",
		Params: map[string]interface{}{},
	})
	if resp.Error == nil || resp.Error.Code != rpc.CodeInvalidParams {
		t.Fatalf("want invalid params, got %+v", resp.Error)
	}
}

func TestScenarioCheckRBACFiltersUnreadable(t *testing.T) {
	ts := newTestServer(t)
	writeScenario(t, ts.memoryRoot, "cog-meta/scenarios/s1.md", "active", "2099-12-31")
	// project-reader has read on projects/** only, deny everywhere else.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "scenario_check",
		Params: map[string]interface{}{"role": "project-reader"},
	})
	if resp.Error != nil {
		t.Fatalf("scenario_check: %v", resp.Error.Message)
	}
	var got scenarioResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Scenarios) != 0 {
		t.Fatalf("project-reader should see 0 scenarios, got %d: %#v", len(got.Scenarios), got.Scenarios)
	}
}

// since accepts the same duration forms as cluster_check/domain_summary
// (resolveSince) — skill playbooks pass "7d"/"30d"/"90d" everywhere.
func TestRecentObservationsDurationSince(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", strings.Join([]string{
		"- 2026-05-29 [health]: slept 8h",
		"",
	}, "\n"))

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "365d"},
	})
	if resp.Error != nil {
		t.Fatalf("recent_observations since=365d: %v", resp.Error.Message)
	}
	var result recentObsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(result.Since) {
		t.Fatalf("since should resolve to a date, got %q", result.Since)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(result.Entries))
	}
}
