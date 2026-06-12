package rpc_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bketelsen/cogmemory/rpc"
)

// These tests exercise the strict-decode contract end-to-end over the socket:
// every handler now runs req.Params through decodeParams (DisallowUnknownFields),
// so an unknown JSON key is a loud -32602 instead of a silent drop. We cover a
// representative spread rather than all ~25 methods: a no-params method
// (housekeeping_scan, only `role`), a simple write, two complex aggregates
// (recent_observations, cluster_check), and an empty-params method (domains.list).
//
// Each method asserts three things where applicable:
//   - an unknown field is rejected with -32602 and the message names the field,
//   - the same call with only valid fields still succeeds (regression),
//   - empty/absent params is still accepted for methods that allow it.

// assertUnknownFieldRejected sends a request whose params include a bogus key
// and asserts the response is -32602 with the offending field named (Go's
// strict decoder emits `json: unknown field "<name>"`).
func assertUnknownFieldRejected(t *testing.T, ts *testServer, id int, method, badField string, params map[string]interface{}) {
	t.Helper()
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: id, Method: method, Params: params,
	})
	if resp.Error == nil {
		t.Fatalf("%s: unknown field %q accepted, want -32602; result=%s", method, badField, string(resp.Result))
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Fatalf("%s: error code = %d, want %d (%s)", method, resp.Error.Code, rpc.CodeInvalidParams, resp.Error.Message)
	}
	want := fmt.Sprintf("unknown field %q", badField)
	if !strings.Contains(resp.Error.Message, want) {
		t.Fatalf("%s: error message should contain %q, got: %s", method, want, resp.Error.Message)
	}
}

// write — simplest required-param handler. Unknown key rejected; valid works.
func TestStrictParamsWrite(t *testing.T) {
	ts := newTestServer(t)

	// `contents` (plural) is a plausible typo for `content` — must be loud now.
	assertUnknownFieldRejected(t, ts, 1, "write", "contents", map[string]interface{}{
		"role": "siona", "path": "notes.md", "content": "hi\n", "contents": "oops",
	})

	// Valid params still write.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "write",
		Params: map[string]interface{}{"role": "siona", "path": "notes.md", "content": "hi\n"},
	})
	if resp.Error != nil {
		t.Fatalf("valid write rejected: %v", resp.Error.Message)
	}
}

// recent_observations — complex aggregate. This is the method behind the
// original bug report (days:→since:). NOTE: as of the rename PR (#22)
// `domain` is now the CANONICAL scope param (with `by_domain` kept as a
// deprecated alias), so `domain` is no longer a "wrong name" — it filters.
// The strict-decode contract is therefore exercised here with `days` (still
// not a field; the window param is `since`) and a clearly-bogus field.
func TestStrictParamsRecentObservations(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", "- 2026-05-29 [x]: p\n")

	// `days` is not a field (the window param is `since`) — rejected.
	assertUnknownFieldRejected(t, ts, 1, "recent_observations", "days", map[string]interface{}{
		"role": "siona", "days": 14,
	})
	// A clearly-unknown field is rejected. (`domain` is now canonical post-#22,
	// so it can no longer stand in as the "wrong name" example here.)
	assertUnknownFieldRejected(t, ts, 2, "recent_observations", "bogus_field", map[string]interface{}{
		"role": "siona", "bogus_field": "x",
	})

	// The correct field names still work end-to-end (both the canonical
	// `domain` and the deprecated `by_domain` alias scope identically; this
	// asserts the alias path, which remains supported until 2026-07-12).
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "since": "2026-05-01", "by_domain": "personal"},
	})
	if resp.Error != nil {
		t.Fatalf("valid recent_observations rejected: %v", resp.Error.Message)
	}
	var result recentObsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Domain != "personal" {
		t.Fatalf("valid by_domain scope wrong: %+v", result.Entries)
	}
}

// cluster_check — complex aggregate with several optional numeric params.
func TestStrictParamsClusterCheck(t *testing.T) {
	ts := newTestServer(t)
	seedObs(t, ts, "personal/observations.md", "- 2026-05-29 [x]: p\n")

	// `min_cluster` is a plausible typo for `min_cluster_size` — rejected.
	assertUnknownFieldRejected(t, ts, 1, "cluster_check", "min_cluster", map[string]interface{}{
		"role": "siona", "min_cluster": 3,
	})

	// Valid params (including a real optional) still succeed.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "cluster_check",
		Params: map[string]interface{}{"role": "siona", "since": "2026-05-01", "min_cluster_size": 2},
	})
	if resp.Error != nil {
		t.Fatalf("valid cluster_check rejected: %v", resp.Error.Message)
	}
}

// housekeeping_scan — params struct has only the embedded `role`. A no-extra
// method is exactly where a silently-dropped key is most surprising.
func TestStrictParamsHousekeepingScan(t *testing.T) {
	ts := newTestServer(t)

	// Any extra key beyond `role` is rejected.
	assertUnknownFieldRejected(t, ts, 1, "housekeeping_scan", "domain", map[string]interface{}{
		"role": "siona", "domain": "personal",
	})

	// Valid (role-only) still works.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "housekeeping_scan",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("valid housekeeping_scan rejected: %v", resp.Error.Message)
	}
}

// domains.list — exercises both the unknown-field rejection and the
// empty/absent-params path (the struct has only `role`, and a caller may send
// no params at all).
func TestStrictParamsDomainsList(t *testing.T) {
	ts := newTestServer(t)

	// Unknown key rejected.
	assertUnknownFieldRejected(t, ts, 1, "domains.list", "filter", map[string]interface{}{
		"role": "siona", "filter": "x",
	})

	// Valid role-only call works.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 2, Method: "domains.list",
		Params: map[string]interface{}{"role": "siona"},
	})
	if resp.Error != nil {
		t.Fatalf("valid domains.list rejected: %v", resp.Error.Message)
	}

	// Absent params entirely (params: null on the wire) must still be accepted
	// — decodeParams treats an empty payload as a no-op. domains.list then runs
	// with role="" and RBAC-filters to whatever that policy allows (here: the
	// empty default policy yields no visible domains, but crucially no error).
	resp = call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 3, Method: "domains.list",
		// Params left nil -> marshals to "params":null -> empty RawMessage.
	})
	if resp.Error != nil {
		t.Fatalf("domains.list with no params should not error, got: %v", resp.Error.Message)
	}
}

// TestRecentObservationsWrongParamNamesAreRejected is the post-strict-decode
// successor to PR #21's TestRecentObservationsWrongParamNamesAreSilentlyIgnored.
//
// PR #21 (test-only) *characterized* the trap: callers reached for `domain:`
// and `days:` out of habit, but at the time the scope param was `by_domain`
// and the window param was `since`. Under a plain json.Unmarshal those
// unknown keys were silently dropped — the call quietly degraded to "all
// domains, default window". That silent drop was the bug behind the
// 2026-06-11 "domain filter is a no-op" report.
//
// Two things changed since: PR #23 makes every handler decode strictly, so
// unknown names now fail loudly with -32602 (the message naming the offending
// field); and PR #22 promoted `domain` to the canonical scope param (keeping
// `by_domain` as a deprecated alias). So `domain` is no longer a wrong name —
// this test exercises the strict path with names that are still genuinely
// unknown (`days`, `frob`). It pins the *new* contract.
//
// SEQUENCING: if PR #21 merges before this PR, its assertion that the wrong
// names are silently ignored will start FAILING on main (that's the intended
// behavior flip). Resolving the rebase means replacing PR #21's
// `...AreSilentlyIgnored` test with this `...AreRejected` one — they assert
// opposite behaviors of the same call and must not coexist.
func TestRecentObservationsWrongParamNamesAreRejected(t *testing.T) {
	ts := newTestServer(t)
	// Two domains both inside the default 7-day window, mirroring PR #21's
	// fixture so the contrast is exact: there the dropped wrong names returned
	// both; here the request never runs because decode fails first.
	today := time.Now().UTC().Format("2006-01-02")
	seedObs(t, ts, "personal/observations.md", fmt.Sprintf("- %s [x]: p\n", today))
	seedObs(t, ts, "work/microsoft/observations.md", fmt.Sprintf("- %s [x]: w\n", today))

	// Two muscle-memory wrong names. `days` is the classic typo for the
	// window param `since`; `frob` stands in for any field that simply does
	// not exist. NOTE: post-#22 `domain` is now the CANONICAL scope param, so
	// it is NO LONGER a wrong name (it filters) — hence it cannot appear here
	// as a rejected field. Each name below is genuinely unknown and is now
	// rejected with -32602 — not silently dropped.
	resp := call(t, ts.socketPath, rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "recent_observations",
		Params: map[string]interface{}{"role": "siona", "days": 14, "frob": "x"},
	})
	if resp.Error == nil {
		t.Fatalf("wrong param names accepted, want -32602; result=%s", string(resp.Result))
	}
	if resp.Error.Code != rpc.CodeInvalidParams {
		t.Fatalf("error code = %d, want %d (%s)", resp.Error.Code, rpc.CodeInvalidParams, resp.Error.Message)
	}
	// Go's strict decoder reports the first unknown field it hits. Map key
	// order on the wire isn't deterministic, so accept either offender as
	// long as one of the two bad names is surfaced.
	if !strings.Contains(resp.Error.Message, `unknown field "days"`) &&
		!strings.Contains(resp.Error.Message, `unknown field "frob"`) {
		t.Fatalf("error should name a bad field (days/frob), got: %s", resp.Error.Message)
	}

	// And the correct names still scope the scan (the contract the caller
	// should have used all along).
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
