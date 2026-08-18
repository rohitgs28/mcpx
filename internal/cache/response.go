package cache

import "encoding/json"

// envelope is the JSON-RPC response shape the cache needs to reason about.
// Result and Error stay raw so a cached body round-trips byte-for-byte apart
// from the id we rewrite. Field order here is the order Marshal emits, which
// keeps re-serialized responses looking like the originals.
type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// Cacheable reports whether a backend response body may be stored.
//
// Only a well-formed JSON-RPC success result qualifies. Transport-level
// failures are filtered by the caller (non-200, streaming, oversized); this
// filters the protocol-level ones:
//
//   - a JSON-RPC error object — the call failed
//   - an MCP result with "isError": true — the tool itself reported failure
//   - anything that is not a single JSON-RPC response object (batches
//     included: their per-request ids cannot be rewritten as a unit)
//
// Caching a failure would pin a transient blip for the whole TTL, which is
// exactly the moment a caller most wants a fresh attempt.
func Cacheable(body []byte) bool {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	if env.Error != nil || len(env.Result) == 0 {
		return false
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	// A result that is not an object (or lacks isError) leaves IsError false.
	_ = json.Unmarshal(env.Result, &res)
	return !res.IsError
}

// RetargetID returns body with its JSON-RPC id replaced by id, so a response
// cached for one request can answer another. A JSON-RPC client matches replies
// to requests by id; serving a cached body with a stale id would look like a
// reply to a request the client never made.
//
// The original bytes are returned unchanged when the id already matches or the
// body cannot be re-encoded, so the caller always has something to serve.
func RetargetID(body []byte, id json.RawMessage) []byte {
	if len(id) == 0 {
		return body
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return body
	}
	if string(env.ID) == string(id) {
		return body
	}
	env.ID = id
	out, err := json.Marshal(env)
	if err != nil {
		return body
	}
	return out
}
