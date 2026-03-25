package policy_test

import (
	"encoding/json"
	"testing"

	"github.com/rohitgs28/mcpx/internal/config"
	"github.com/rohitgs28/mcpx/internal/mcp"
	"github.com/rohitgs28/mcpx/internal/policy"
)

func makeRequest(method string, toolName string) *mcp.Request {
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
	}
	if toolName != "" {
		params := mcp.ToolCallParams{Name: toolName}
		data, _ := json.Marshal(params)
		req.Params = data
	}
	return req
}

func TestEvaluate_NoPolicy(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "test", URL: "http://localhost:3000"},
	}
	e := policy.New(servers)
	req := makeRequest(mcp.MethodToolsCall, "read_file")

	result := e.Evaluate("test", req)
	if !result.Allowed {
		t.Fatal("expected allowed when no policy restrictions are set")
	}
}

func TestEvaluate_UnknownServer(t *testing.T) {
	e := policy.New(nil)
	req := makeRequest(mcp.MethodToolsCall, "read_file")

	result := e.Evaluate("nonexistent", req)
	if !result.Allowed {
		t.Fatal("expected allowed for unknown server (fail-open)")
	}
}

func TestEvaluate_ReadOnly_BlocksToolsCall(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "db", URL: "http://localhost:3002", Policy: config.Policy{ReadOnly: true}},
	}
	e := policy.New(servers)
	req := makeRequest(mcp.MethodToolsCall, "execute_query")

	result := e.Evaluate("db", req)
	if result.Allowed {
		t.Fatal("expected denied for tools/call on read-only server")
	}
	if result.Reason == "" {
		t.Fatal("expected a reason for denial")
	}
}

func TestEvaluate_ReadOnly_AllowsToolsList(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "db", URL: "http://localhost:3002", Policy: config.Policy{ReadOnly: true}},
	}
	e := policy.New(servers)
	req := makeRequest(mcp.MethodToolsList, "")

	result := e.Evaluate("db", req)
	if !result.Allowed {
		t.Fatal("expected allowed for tools/list even in read-only mode")
	}
}

func TestEvaluate_ReadOnly_AllowsInitialize(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "db", URL: "http://localhost:3002", Policy: config.Policy{ReadOnly: true}},
	}
	e := policy.New(servers)
	req := makeRequest(mcp.MethodInitialize, "")

	result := e.Evaluate("db", req)
	if !result.Allowed {
		t.Fatal("expected allowed for initialize even in read-only mode")
	}
}

func TestEvaluate_DenyList_Blocked(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "fs", URL: "http://localhost:3001", Policy: config.Policy{
			DenyTools: []string{"delete_file", "write_file"},
		}},
	}
	e := policy.New(servers)

	result := e.Evaluate("fs", makeRequest(mcp.MethodToolsCall, "delete_file"))
	if result.Allowed {
		t.Fatal("expected denied for tool in deny list")
	}

	result = e.Evaluate("fs", makeRequest(mcp.MethodToolsCall, "write_file"))
	if result.Allowed {
		t.Fatal("expected denied for tool in deny list")
	}
}

func TestEvaluate_DenyList_Allowed(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "fs", URL: "http://localhost:3001", Policy: config.Policy{
			DenyTools: []string{"delete_file", "write_file"},
		}},
	}
	e := policy.New(servers)

	result := e.Evaluate("fs", makeRequest(mcp.MethodToolsCall, "read_file"))
	if !result.Allowed {
		t.Fatal("expected allowed for tool not in deny list")
	}
}

func TestEvaluate_AllowList_Allowed(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "fs", URL: "http://localhost:3001", Policy: config.Policy{
			AllowTools: []string{"read_file", "list_directory", "search_files"},
		}},
	}
	e := policy.New(servers)

	for _, tool := range []string{"read_file", "list_directory", "search_files"} {
		result := e.Evaluate("fs", makeRequest(mcp.MethodToolsCall, tool))
		if !result.Allowed {
			t.Fatalf("expected allowed for %q in allow list", tool)
		}
	}
}

func TestEvaluate_AllowList_Blocked(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "fs", URL: "http://localhost:3001", Policy: config.Policy{
			AllowTools: []string{"read_file", "list_directory"},
		}},
	}
	e := policy.New(servers)

	result := e.Evaluate("fs", makeRequest(mcp.MethodToolsCall, "write_file"))
	if result.Allowed {
		t.Fatal("expected denied for tool not in allow list")
	}
}

func TestEvaluate_NonToolsCall_AlwaysAllowed(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "fs", URL: "http://localhost:3001", Policy: config.Policy{
			AllowTools: []string{"read_file"},
			DenyTools:  []string{"write_file"},
		}},
	}
	e := policy.New(servers)

	methods := []string{
		mcp.MethodToolsList, mcp.MethodInitialize, mcp.MethodPing,
		mcp.MethodResourceRead, mcp.MethodResourceList,
		mcp.MethodPromptGet, mcp.MethodPromptList,
	}
	for _, m := range methods {
		result := e.Evaluate("fs", makeRequest(m, ""))
		if !result.Allowed {
			t.Fatalf("expected %s to always be allowed", m)
		}
	}
}

func TestEvaluate_MultipleServers(t *testing.T) {
	servers := []config.ServerConfig{
		{Name: "fs", URL: "http://localhost:3001", Policy: config.Policy{
			DenyTools: []string{"delete_file"},
		}},
		{Name: "db", URL: "http://localhost:3002", Policy: config.Policy{
			ReadOnly: true,
		}},
	}
	e := policy.New(servers)

	result := e.Evaluate("fs", makeRequest(mcp.MethodToolsCall, "delete_file"))
	if result.Allowed {
		t.Fatal("expected denied on fs")
	}

	result = e.Evaluate("db", makeRequest(mcp.MethodToolsCall, "delete_file"))
	if result.Allowed {
		t.Fatal("expected denied on read-only db")
	}
}
