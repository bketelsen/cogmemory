package rpc

import (
	"bytes"
	"encoding/json"
)

// decodeParams strictly unmarshals req.Params into p, rejecting unknown
// fields so a typo in any optional param is a loud -32602 instead of being
// silently dropped. This guards the whole class of "I called it with
// `domain:` but the real field is `by_domain:` and it silently degraded"
// bugs (see PR #21).
//
// An empty/absent params payload is a no-op (p keeps its zero value), so
// methods with only optional fields — and no-param methods like domains.list
// — still work when called with no params at all.
func decodeParams(raw json.RawMessage, p interface{}) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(p)
}
