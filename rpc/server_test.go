package rpc_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bketelsen/cogmemory/config"
	"github.com/bketelsen/cogmemory/rbac"
	"github.com/bketelsen/cogmemory/rpc"
	"github.com/bketelsen/cogmemory/store"
)

type testServer struct {
	srv        *rpc.Server
	socketPath string
	ln         net.Listener
	done       chan struct{}
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "memory"))
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
		},
	}
	r := rbac.New(cfg)
	srv := rpc.New(s, r)

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

	return &testServer{srv: srv, socketPath: socketPath, ln: ln, done: done}
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
