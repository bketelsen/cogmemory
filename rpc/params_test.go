package rpc

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeParams is the single strict-decode chokepoint every RPC handler runs
// req.Params through. These cases pin its three behaviors: empty payload is a
// no-op, a well-formed payload populates the struct, and an unknown field is
// a hard error (not a silent drop) — the guarantee the whole PR rests on.
func TestDecodeParams(t *testing.T) {
	type sample struct {
		Role string `json:"role"`
		N    int    `json:"n,omitempty"`
	}

	t.Run("empty params is a no-op", func(t *testing.T) {
		// Both nil and zero-length raw payloads must leave p at its zero
		// value with no error — methods with only optional fields (and
		// no-param methods) rely on this.
		for _, raw := range []json.RawMessage{nil, {}, json.RawMessage("")} {
			var p sample
			if err := decodeParams(raw, &p); err != nil {
				t.Fatalf("decodeParams(%q) = %v, want nil", string(raw), err)
			}
			if p.Role != "" || p.N != 0 {
				t.Fatalf("empty params mutated p: %+v", p)
			}
		}
	})

	t.Run("valid params decode", func(t *testing.T) {
		var p sample
		if err := decodeParams(json.RawMessage(`{"role":"siona","n":3}`), &p); err != nil {
			t.Fatalf("decodeParams valid = %v, want nil", err)
		}
		if p.Role != "siona" || p.N != 3 {
			t.Fatalf("decoded p = %+v, want {Role:siona N:3}", p)
		}
	})

	t.Run("unknown field is a hard error naming the field", func(t *testing.T) {
		var p sample
		err := decodeParams(json.RawMessage(`{"role":"siona","bogus":1}`), &p)
		if err == nil {
			t.Fatal("decodeParams with unknown field = nil, want error")
		}
		// Go's strict decoder names the offending field; downstream handlers
		// surface this verbatim in the -32602 message, so assert it's there.
		if !strings.Contains(err.Error(), `unknown field "bogus"`) {
			t.Fatalf("error should name the bad field, got: %v", err)
		}
	})
}
