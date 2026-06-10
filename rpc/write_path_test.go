package rpc_test

import (
	"strings"
	"testing"

	"github.com/bketelsen/cogmemory/rpc"
)

// Writes whose first segment is a domain *id* with a different configured
// path (e.g. "dakota/INDEX.md" when dakota lives at projects/dakota) are the
// id-as-path client mistake — rejected hard with the corrective path.
func TestWriteRejectsIDAsPath(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role": "siona", "path": "dakota/INDEX.md", "content": "# stray\n",
		},
	})
	if resp.Error == nil {
		t.Fatal("write(id-as-path): want error, got success")
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, rpc.CodeInvalidParams)
	}
	if !strings.Contains(resp.Error.Message, "projects/dakota") {
		t.Errorf("error should name configured path: %q", resp.Error.Message)
	}
}

func TestAppendRejectsIDAsPath(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "append",
		Params: map[string]interface{}{
			"role": "siona", "path": "dakota/observations.md",
			"text": "- 2026-06-10 [test]: stray append\n",
		},
	})
	if resp.Error == nil {
		t.Fatal("append(id-as-path): want error, got success")
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, rpc.CodeInvalidParams)
	}
	if !strings.Contains(resp.Error.Message, "projects/dakota") {
		t.Errorf("error should name configured path: %q", resp.Error.Message)
	}
}

// Undeclared files under a valid domain path remain warn-only: the write
// succeeds (INDEX.md is not in dakota's declared files).
func TestWriteUndeclaredFileUnderDomainPathStillAllowed(t *testing.T) {
	ts := newTestServer(t)

	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "write",
		Params: map[string]interface{}{
			"role": "siona", "path": "projects/dakota/INDEX.md", "content": "# index\n",
		},
	})
	if resp.Error != nil {
		t.Fatalf("write(undeclared file under domain path) should warn-only, got: %v", resp.Error.Message)
	}
}
