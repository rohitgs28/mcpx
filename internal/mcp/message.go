// Package mcp defines MCP protocol message types for the gateway.
// The gateway needs to understand MCP messages to enforce tool-level
// policies, log audit trails, and apply rate limits per tool.
package mcp

import "encoding/json"

// Request represents a JSON-RPC 2.0 request used by MCP.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Notification represents a JSON-RPC notification (no ID).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ToolCallParams represents the params for a tools/call request.
type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ToolsListResult represents the result payload of a tools/list response.
// Each tool is kept as a raw message so the gateway can hash the exact bytes
// the upstream advertised (name, description, and full input schema) and
// re-serialize the surviving tools without lossy round-tripping.
type ToolsListResult struct {
	Tools      []json.RawMessage `json:"tools"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

// ParseToolsListResult extracts the tools array from a tools/list result.
func ParseToolsListResult(result json.RawMessage) (*ToolsListResult, error) {
	var r ToolsListResult
	if err := json.Unmarshal(result, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ToolName extracts the "name" field from a raw tool object, or "" if absent.
func ToolName(raw json.RawMessage) string {
	var t struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &t)
	return t.Name
}

const (
	MethodToolsCall    = "tools/call"
	MethodToolsList    = "tools/list"
	MethodInitialize   = "initialize"
	MethodInitialized  = "notifications/initialized"
	MethodPing         = "ping"
	MethodResourceRead = "resources/read"
	MethodResourceList = "resources/list"
	MethodPromptGet    = "prompts/get"
	MethodPromptList   = "prompts/list"
)

func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	if req.Method == "" {
		return nil, nil
	}
	return &req, nil
}

func ParseToolCall(req *Request) (*ToolCallParams, error) {
	if req.Method != MethodToolsCall {
		return nil, nil
	}
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, err
	}
	return &params, nil
}

func NewErrorResponse(id any, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}
