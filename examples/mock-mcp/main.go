// Command mock-mcp is a tiny, dependency-free MCP server for trying out mcpx.
//
// It speaks just enough of the MCP JSON-RPC protocol over HTTP to exercise the
// gateway: initialize, tools/list, and tools/call. It deliberately advertises a
// "dangerous_delete" tool so the demo config can show policy filtering and
// (by editing a tool) tool-integrity pinning in action.
//
//	go run ./examples/mock-mcp            # listens on :3001
//	go run ./examples/mock-mcp -addr :3005
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
)

var tools = []map[string]any{
	{
		"name":        "echo",
		"description": "Echo back the provided text.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
			"required":   []string{"text"},
		},
	},
	{
		"name":        "read_file",
		"description": "Read a file from disk.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	},
	{
		"name":        "dangerous_delete",
		"description": "Delete a file. The demo policy denies this tool.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	},
}

func main() {
	addr := flag.String("addr", ":3001", "listen address")
	flag.Parse()

	http.HandleFunc("/", handle)
	log.Printf("mock MCP server listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func handle(w http.ResponseWriter, r *http.Request) {
	// GET is used by the gateway's health probe.
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	var result any
	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]string{"name": "mock-mcp", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}
	case "tools/list":
		result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name string `json:"name"`
		}
		json.Unmarshal(req.Params, &p)
		result = map[string]any{
			"content": []map[string]string{{"type": "text", "text": "mock-mcp executed tool: " + p.Name}},
		}
	default:
		result = map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}
