// Package policy enforces tool-level access control on MCP requests.
// It evaluates allow/deny lists and read-only modes per backend server.
package policy

import (
	"fmt"

	"github.com/rohitgs28/mcpx/internal/config"
	"github.com/rohitgs28/mcpx/internal/mcp"
)

type Engine struct {
	policies map[string]config.Policy
}

func New(servers []config.ServerConfig) *Engine {
	policies := make(map[string]config.Policy)
	for _, s := range servers { policies[s.Name] = s.Policy }
	return &Engine{policies: policies}
}

type Result struct { Allowed bool; Reason string }

func (e *Engine) Evaluate(serverName string, req *mcp.Request) Result {
	policy, ok := e.policies[serverName]
	if !ok { return Result{Allowed: true} }
	if policy.ReadOnly && req.Method == mcp.MethodToolsCall {
		return Result{Allowed: false, Reason: fmt.Sprintf("server %q is in read-only mode: tools/call is blocked", serverName)}
	}
	if req.Method != mcp.MethodToolsCall { return Result{Allowed: true} }
	tc, err := mcp.ParseToolCall(req)
	if err != nil || tc == nil { return Result{Allowed: true} }
	for _, d := range policy.DenyTools {
		if d == tc.Name { return Result{Allowed: false, Reason: fmt.Sprintf("tool %q is denied on server %q", tc.Name, serverName)} }
	}
	if len(policy.AllowTools) > 0 {
		for _, a := range policy.AllowTools { if a == tc.Name { return Result{Allowed: true} } }
		return Result{Allowed: false, Reason: fmt.Sprintf("tool %q not in allow list for %q", tc.Name, serverName)}
	}
	return Result{Allowed: true}
}
