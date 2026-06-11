package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as the fake MCP stdio server: when re-executed with
// MCPX_STDIO_HELPER set, the test binary speaks newline-delimited JSON-RPC
// on stdin/stdout instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("MCPX_STDIO_HELPER") == "1" {
		helperMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// helperMain implements a minimal MCP stdio server with test hooks:
// test/initCount and test/notifyCount expose per-process handshake counters,
// test/crash exits abruptly, test/sleep delays its reply.
func helperMain() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	out := json.NewEncoder(os.Stdout)
	initCount, notifyCount := 0, 0
	for sc.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		reply := func(result any) {
			out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		}
		switch msg.Method {
		case "initialize":
			initCount++
			reply(map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "helper"}})
		case "notifications/initialized":
			notifyCount++
		case "test/initCount":
			reply(initCount)
		case "test/notifyCount":
			reply(notifyCount)
		case "test/crash":
			os.Exit(1)
		case "test/sleep":
			var ms int
			json.Unmarshal(msg.Params, &ms)
			time.Sleep(time.Duration(ms) * time.Millisecond)
			reply("slept")
		case "echo":
			reply(json.RawMessage(msg.Params))
		case "tools/list":
			reply(map[string]any{"tools": []map[string]any{{"name": "echo"}, {"name": "secret"}}})
		default:
			out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "unknown method"}})
		}
	}
}

func helperConfig(t *testing.T) Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return Config{
		Command: exe,
		Env:     map[string]string{"MCPX_STDIO_HELPER": "1"},
	}
}

func helperClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient("test", helperConfig(t))
	t.Cleanup(func() { c.Close() })
	return c
}

func call(t *testing.T, c *Client, body string) map[string]json.RawMessage {
	t.Helper()
	resp, err := c.Call(context.Background(), []byte(body))
	if err != nil {
		t.Fatalf("Call(%s): %v", body, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(resp, &m); err != nil {
		t.Fatalf("decode response %q: %v", resp, err)
	}
	return m
}

func initialize(t *testing.T, c *Client) {
	t.Helper()
	call(t, c, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
}

func intResult(t *testing.T, m map[string]json.RawMessage) int {
	t.Helper()
	var n int
	if err := json.Unmarshal(m["result"], &n); err != nil {
		t.Fatalf("non-integer result %s: %v", m["result"], err)
	}
	return n
}

func TestCallRestoresCallerID(t *testing.T) {
	c := helperClient(t)
	for _, id := range []string{`"abc-123"`, `42`, `0`} {
		resp := call(t, c, `{"jsonrpc":"2.0","id":`+id+`,"method":"echo","params":{"x":1}}`)
		if got := strings.TrimSpace(string(resp["id"])); got != id {
			t.Errorf("response id = %s, want %s", got, id)
		}
		if string(resp["result"]) != `{"x":1}` {
			t.Errorf("result = %s, want {\"x\":1}", resp["result"])
		}
	}
}

func TestInitializeServedFromCache(t *testing.T) {
	c := helperClient(t)
	initialize(t, c)
	// A second client initializing must not re-handshake the shared child.
	resp := call(t, c, `{"jsonrpc":"2.0","id":"second","method":"initialize","params":{}}`)
	if !strings.Contains(string(resp["result"]), "helper") {
		t.Fatalf("cached initialize result = %s", resp["result"])
	}
	if got := intResult(t, call(t, c, `{"jsonrpc":"2.0","id":3,"method":"test/initCount"}`)); got != 1 {
		t.Errorf("child saw %d initialize requests, want 1", got)
	}
}

func TestDuplicateInitializedNotificationSwallowed(t *testing.T) {
	c := helperClient(t)
	initialize(t, c)
	for i := 0; i < 3; i++ {
		if err := c.Notify([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
			t.Fatalf("Notify: %v", err)
		}
	}
	if got := intResult(t, call(t, c, `{"jsonrpc":"2.0","id":4,"method":"test/notifyCount"}`)); got != 1 {
		t.Errorf("child saw %d initialized notifications, want 1", got)
	}
}

func TestCrashRespawnReplaysHandshake(t *testing.T) {
	c := helperClient(t)
	initialize(t, c)
	if err := c.Notify([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, err := c.Call(context.Background(), []byte(`{"jsonrpc":"2.0","id":5,"method":"test/crash"}`)); err == nil {
		t.Fatal("expected error from crashed child")
	}
	// Wait out the respawn backoff, then verify the fresh child was
	// re-initialized by the gateway, not by any client.
	time.Sleep(respawnBackoff + 100*time.Millisecond)
	if got := intResult(t, call(t, c, `{"jsonrpc":"2.0","id":6,"method":"test/initCount"}`)); got != 1 {
		t.Errorf("respawned child saw %d initialize requests, want 1 (replayed)", got)
	}
	if got := intResult(t, call(t, c, `{"jsonrpc":"2.0","id":7,"method":"test/notifyCount"}`)); got != 1 {
		t.Errorf("respawned child saw %d initialized notifications, want 1 (replayed)", got)
	}
}

func TestRequestTimeout(t *testing.T) {
	cfg := helperConfig(t)
	cfg.RequestTimeout = 100 * time.Millisecond
	c := NewClient("test", cfg)
	t.Cleanup(func() { c.Close() })
	start := time.Now()
	_, err := c.Call(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"test/sleep","params":5000}`))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout took %s, want ~100ms", elapsed)
	}
}

func TestConcurrentCalls(t *testing.T) {
	c := helperClient(t)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"echo","params":{"n":%d}}`, i, i)
			resp, err := c.Call(context.Background(), []byte(body))
			if err != nil {
				errs <- err
				return
			}
			var m struct {
				ID     int            `json:"id"`
				Result map[string]int `json:"result"`
			}
			if err := json.Unmarshal(resp, &m); err != nil {
				errs <- err
				return
			}
			if m.ID != i || m.Result["n"] != i {
				errs <- fmt.Errorf("call %d got id=%d n=%d", i, m.ID, m.Result["n"])
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestManagerEnsureReusesUnchanged(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)
	cfg := helperConfig(t)
	a := m.Ensure("s", cfg)
	if b := m.Ensure("s", cfg); b != a {
		t.Error("unchanged config should reuse the client")
	}
	cfg.Args = []string{"-changed"}
	if b := m.Ensure("s", cfg); b == a {
		t.Error("changed config should replace the client")
	}
}

func TestManagerPrune(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)
	m.Ensure("keep", helperConfig(t))
	m.Ensure("drop", helperConfig(t))
	m.Prune(map[string]bool{"keep": true})
	if _, ok := m.clients["drop"]; ok {
		t.Error("pruned client still registered")
	}
	if _, ok := m.clients["keep"]; !ok {
		t.Error("active client was pruned")
	}
}
