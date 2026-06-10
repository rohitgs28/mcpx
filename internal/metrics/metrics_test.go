package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecordRequest(t *testing.T) {
	c := New()
	c.RecordRequest("filesystem", "tools/call", 200, 50*time.Millisecond)
	c.RecordRequest("filesystem", "tools/call", 200, 100*time.Millisecond)
	c.RecordRequest("database", "tools/list", 403, 5*time.Millisecond)

	body := getMetrics(t, c)

	if !strings.Contains(body, `mcpx_requests_total{server="filesystem",method="tools/call",status_code="2xx"} 2`) {
		t.Error("expected 2 requests for filesystem tools/call 2xx")
	}
	if !strings.Contains(body, `mcpx_requests_total{server="database",method="tools/list",status_code="4xx"} 1`) {
		t.Error("expected 1 request for database tools/list 4xx")
	}
}

func TestRecordToolCall(t *testing.T) {
	c := New()
	c.RecordToolCall("filesystem", "read_file", "allow", "")
	c.RecordToolCall("filesystem", "delete_file", "deny", "")
	c.RecordToolCall("filesystem", "read_file", "allow", "")

	body := getMetrics(t, c)

	if !strings.Contains(body, `mcpx_tool_calls_total{server="filesystem",tool="read_file",decision="allow"} 2`) {
		t.Error("expected 2 allowed read_file calls")
	}
	if !strings.Contains(body, `mcpx_tool_calls_total{server="filesystem",tool="delete_file",decision="deny"} 1`) {
		t.Error("expected 1 denied delete_file call")
	}
	if !strings.Contains(body, `mcpx_policy_decisions_total{server="filesystem",decision="allow"} 2`) {
		t.Error("expected 2 allow decisions for filesystem")
	}
}

func TestAuthAndRateLimitCounters(t *testing.T) {
	c := New()
	c.RecordAuthFailure()
	c.RecordAuthFailure()
	c.RecordRateLimitHit()

	body := getMetrics(t, c)

	if !strings.Contains(body, "mcpx_auth_failures_total 2") {
		t.Error("expected 2 auth failures")
	}
	if !strings.Contains(body, "mcpx_rate_limit_hits_total 1") {
		t.Error("expected 1 rate limit hit")
	}
}

func TestActiveConnections(t *testing.T) {
	c := New()
	c.IncrActiveConnections()
	c.IncrActiveConnections()
	c.IncrActiveConnections()
	c.DecrActiveConnections()

	body := getMetrics(t, c)

	if !strings.Contains(body, "mcpx_active_connections 2") {
		t.Error("expected 2 active connections")
	}
}

func TestLatencyBuckets(t *testing.T) {
	c := New()
	c.RecordRequest("test-server", "tools/call", 200, 50*time.Millisecond)

	body := getMetrics(t, c)

	// 50ms should be in the 50, 100, 250, 500, 1000, 5000, 10000, +Inf buckets
	if !strings.Contains(body, `mcpx_request_duration_ms_bucket{server="test-server",le="50"} 1`) {
		t.Error("expected 50ms request in le=50 bucket")
	}
	if !strings.Contains(body, `mcpx_request_duration_ms_bucket{server="test-server",le="+Inf"} 1`) {
		t.Error("expected 50ms request in +Inf bucket")
	}
}

func TestServersRegistered(t *testing.T) {
	c := New()
	c.SetServersRegistered(3)

	body := getMetrics(t, c)

	if !strings.Contains(body, "mcpx_servers_registered 3") {
		t.Error("expected 3 servers registered")
	}
}

func TestMetricsContentType(t *testing.T) {
	c := New()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	c.Handler()(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content type, got %s", ct)
	}
}

func TestUptimeIncreases(t *testing.T) {
	c := New()
	time.Sleep(10 * time.Millisecond)

	body := getMetrics(t, c)

	if !strings.Contains(body, "mcpx_uptime_seconds") {
		t.Error("expected uptime metric")
	}
}

func getMetrics(t *testing.T, c *Collector) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	c.Handler()(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	return w.Body.String()
}
