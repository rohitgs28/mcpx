// Package metrics provides Prometheus-compatible metrics for mcpx.
//
// Exposes request counts, latencies, tool usage, policy decisions,
// and per-server stats at /metrics in Prometheus exposition format.
// Zero external dependencies — uses only the Go standard library.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Collector holds all gateway metrics in memory.
type Collector struct {
	mu sync.RWMutex

	// Counters
	requestsTotal    map[string]*atomic.Int64 // label: server,method,status
	toolCallsTotal   map[string]*atomic.Int64 // label: server,tool,decision
	policyDecisions  map[string]*atomic.Int64 // label: server,decision (allow/deny)
	authFailures     atomic.Int64
	rateLimitHits    atomic.Int64

	// Histograms (simplified: count + sum for averages, plus buckets)
	latencySum       map[string]*atomic.Int64 // microseconds, label: server
	latencyCount     map[string]*atomic.Int64 // label: server
	latencyBuckets   map[string]map[string]*atomic.Int64 // label: server -> bucket -> count

	// Gauges
	activeConnections atomic.Int64
	startTime         time.Time
	serversRegistered atomic.Int64
}

// bucketBounds defines latency histogram bucket boundaries in milliseconds.
var bucketBounds = []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000, 10000}

// New creates a new metrics Collector.
func New() *Collector {
	return &Collector{
		requestsTotal:   make(map[string]*atomic.Int64),
		toolCallsTotal:  make(map[string]*atomic.Int64),
		policyDecisions: make(map[string]*atomic.Int64),
		latencySum:      make(map[string]*atomic.Int64),
		latencyCount:    make(map[string]*atomic.Int64),
		latencyBuckets:  make(map[string]map[string]*atomic.Int64),
		startTime:       time.Now(),
	}
}

// RecordRequest records a proxied request with its outcome.
func (c *Collector) RecordRequest(server, method string, status int, duration time.Duration) {
	// Request counter
	label := fmt.Sprintf("server=%q,method=%q,status_code=%q", server, method, statusGroup(status))
	c.getOrCreateCounter(c.requestsTotal, label).Add(1)

	// Latency histogram
	us := duration.Microseconds()
	c.getOrCreateCounter(c.latencySum, server).Add(us)
	c.getOrCreateCounter(c.latencyCount, server).Add(1)

	ms := float64(duration.Milliseconds())
	c.mu.Lock()
	if _, ok := c.latencyBuckets[server]; !ok {
		c.latencyBuckets[server] = make(map[string]*atomic.Int64)
	}
	for _, bound := range bucketBounds {
		if ms <= bound {
			key := fmt.Sprintf("%.0f", bound)
			if _, ok := c.latencyBuckets[server][key]; !ok {
				c.latencyBuckets[server][key] = &atomic.Int64{}
			}
			c.latencyBuckets[server][key].Add(1)
		}
	}
	// +Inf bucket
	key := "+Inf"
	if _, ok := c.latencyBuckets[server][key]; !ok {
		c.latencyBuckets[server][key] = &atomic.Int64{}
	}
	c.latencyBuckets[server][key].Add(1)
	c.mu.Unlock()
}

// RecordToolCall records a tool invocation and its policy decision.
func (c *Collector) RecordToolCall(server, tool, decision string) {
	label := fmt.Sprintf("server=%q,tool=%q,decision=%q", server, tool, decision)
	c.getOrCreateCounter(c.toolCallsTotal, label).Add(1)

	policyLabel := fmt.Sprintf("server=%q,decision=%q", server, decision)
	c.getOrCreateCounter(c.policyDecisions, policyLabel).Add(1)
}

// RecordAuthFailure increments the auth failure counter.
func (c *Collector) RecordAuthFailure() {
	c.authFailures.Add(1)
}

// RecordRateLimitHit increments the rate limit counter.
func (c *Collector) RecordRateLimitHit() {
	c.rateLimitHits.Add(1)
}

// SetActiveConnections sets the current number of active connections.
func (c *Collector) SetActiveConnections(n int64) {
	c.activeConnections.Store(n)
}

// IncrActiveConnections atomically increments active connections.
func (c *Collector) IncrActiveConnections() {
	c.activeConnections.Add(1)
}

// DecrActiveConnections atomically decrements active connections.
func (c *Collector) DecrActiveConnections() {
	c.activeConnections.Add(-1)
}

// SetServersRegistered sets the number of registered backend servers.
func (c *Collector) SetServersRegistered(n int64) {
	c.serversRegistered.Store(n)
}

// Handler returns an http.HandlerFunc that serves /metrics in Prometheus format.
func (c *Collector) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		var b strings.Builder

		// Gateway info
		b.WriteString("# HELP mcpx_info Gateway metadata.\n")
		b.WriteString("# TYPE mcpx_info gauge\n")
		fmt.Fprintf(&b, "mcpx_info{version=\"0.1.0\"} 1\n\n")

		// Uptime
		b.WriteString("# HELP mcpx_uptime_seconds Gateway uptime in seconds.\n")
		b.WriteString("# TYPE mcpx_uptime_seconds gauge\n")
		fmt.Fprintf(&b, "mcpx_uptime_seconds %.1f\n\n", time.Since(c.startTime).Seconds())

		// Active connections
		b.WriteString("# HELP mcpx_active_connections Current number of active connections.\n")
		b.WriteString("# TYPE mcpx_active_connections gauge\n")
		fmt.Fprintf(&b, "mcpx_active_connections %d\n\n", c.activeConnections.Load())

		// Servers registered
		b.WriteString("# HELP mcpx_servers_registered Number of registered backend servers.\n")
		b.WriteString("# TYPE mcpx_servers_registered gauge\n")
		fmt.Fprintf(&b, "mcpx_servers_registered %d\n\n", c.serversRegistered.Load())

		// Request totals
		b.WriteString("# HELP mcpx_requests_total Total number of proxied requests.\n")
		b.WriteString("# TYPE mcpx_requests_total counter\n")
		c.writeCounterMap(&b, "mcpx_requests_total", c.requestsTotal)
		b.WriteString("\n")

		// Tool calls
		b.WriteString("# HELP mcpx_tool_calls_total Total tool invocations by outcome.\n")
		b.WriteString("# TYPE mcpx_tool_calls_total counter\n")
		c.writeCounterMap(&b, "mcpx_tool_calls_total", c.toolCallsTotal)
		b.WriteString("\n")

		// Policy decisions
		b.WriteString("# HELP mcpx_policy_decisions_total Policy evaluation outcomes.\n")
		b.WriteString("# TYPE mcpx_policy_decisions_total counter\n")
		c.writeCounterMap(&b, "mcpx_policy_decisions_total", c.policyDecisions)
		b.WriteString("\n")

		// Auth failures
		b.WriteString("# HELP mcpx_auth_failures_total Total authentication failures.\n")
		b.WriteString("# TYPE mcpx_auth_failures_total counter\n")
		fmt.Fprintf(&b, "mcpx_auth_failures_total %d\n\n", c.authFailures.Load())

		// Rate limit hits
		b.WriteString("# HELP mcpx_rate_limit_hits_total Total rate limit rejections.\n")
		b.WriteString("# TYPE mcpx_rate_limit_hits_total counter\n")
		fmt.Fprintf(&b, "mcpx_rate_limit_hits_total %d\n\n", c.rateLimitHits.Load())

		// Latency histograms
		b.WriteString("# HELP mcpx_request_duration_ms Request latency in milliseconds.\n")
		b.WriteString("# TYPE mcpx_request_duration_ms histogram\n")
		c.mu.RLock()
		servers := make([]string, 0, len(c.latencyBuckets))
		for s := range c.latencyBuckets {
			servers = append(servers, s)
		}
		sort.Strings(servers)
		for _, server := range servers {
			buckets := c.latencyBuckets[server]
			for _, bound := range bucketBounds {
				key := fmt.Sprintf("%.0f", bound)
				if counter, ok := buckets[key]; ok {
					fmt.Fprintf(&b, "mcpx_request_duration_ms_bucket{server=%q,le=\"%.0f\"} %d\n",
						server, bound, counter.Load())
				}
			}
			if counter, ok := buckets["+Inf"]; ok {
				fmt.Fprintf(&b, "mcpx_request_duration_ms_bucket{server=%q,le=\"+Inf\"} %d\n",
					server, counter.Load())
			}
			if sum, ok := c.latencySum[server]; ok {
				fmt.Fprintf(&b, "mcpx_request_duration_ms_sum{server=%q} %.3f\n",
					server, float64(sum.Load())/1000.0)
			}
			if count, ok := c.latencyCount[server]; ok {
				fmt.Fprintf(&b, "mcpx_request_duration_ms_count{server=%q} %d\n",
					server, count.Load())
			}
		}
		c.mu.RUnlock()

		w.Write([]byte(b.String()))
	}
}

// getOrCreateCounter lazily creates atomic counters for label combinations.
func (c *Collector) getOrCreateCounter(m map[string]*atomic.Int64, key string) *atomic.Int64 {
	c.mu.RLock()
	if counter, ok := m[key]; ok {
		c.mu.RUnlock()
		return counter
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if counter, ok := m[key]; ok {
		return counter
	}
	counter := &atomic.Int64{}
	m[key] = counter
	return counter
}

// writeCounterMap writes all entries in a counter map in sorted order.
func (c *Collector) writeCounterMap(b *strings.Builder, name string, m map[string]*atomic.Int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(b, "%s{%s} %d\n", name, k, m[k].Load())
	}
}

// statusGroup maps HTTP status codes to groups for low-cardinality labels.
func statusGroup(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "other"
	}
}
