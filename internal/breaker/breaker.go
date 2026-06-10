// Package breaker implements a classic three-state circuit breaker per
// backend server. When a backend fails repeatedly the breaker opens and the
// gateway fails fast with 503 instead of queueing requests against a dead
// upstream; after a cooldown a limited number of probes test recovery.
package breaker

import (
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Config tunes a breaker. Zero values are replaced with defaults.
type Config struct {
	FailureThreshold int           // consecutive failures that open the breaker (default 5)
	Cooldown         time.Duration // open -> half-open delay (default 30s)
	HalfOpenMax      int           // concurrent probes admitted in half-open (default 1)
}

func (c Config) withDefaults() Config {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 30 * time.Second
	}
	if c.HalfOpenMax <= 0 {
		c.HalfOpenMax = 1
	}
	return c
}

type Breaker struct {
	mu       sync.Mutex
	cfg      Config
	state    State
	failures int       // consecutive failures while closed
	openedAt time.Time // when the breaker last opened
	probes   int       // in-flight probes while half-open
	now      func() time.Time
}

func New(cfg Config) *Breaker {
	return &Breaker{cfg: cfg.withDefaults(), now: time.Now}
}

// Allow reports whether a request may proceed. While open it returns false
// until the cooldown elapses, then transitions to half-open and admits up to
// HalfOpenMax concurrent probes.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if b.now().Sub(b.openedAt) < b.cfg.Cooldown {
			return false
		}
		b.state = StateHalfOpen
		b.probes = 1
		return true
	default: // StateHalfOpen
		if b.probes >= b.cfg.HalfOpenMax {
			return false
		}
		b.probes++
		return true
	}
}

// RecordSuccess closes the breaker (from half-open) or resets the failure
// streak (while closed).
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = StateClosed
	b.failures = 0
	b.probes = 0
}

// RecordFailure counts a failure and reports whether this call tripped the
// breaker open (for logging/metrics on the transition).
func (b *Breaker) RecordFailure() (tripped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateHalfOpen:
		// Failed probe: reopen and restart the cooldown.
		b.state = StateOpen
		b.openedAt = b.now()
		b.probes = 0
		return true
	case StateClosed:
		b.failures++
		if b.failures >= b.cfg.FailureThreshold {
			b.state = StateOpen
			b.openedAt = b.now()
			return true
		}
	}
	return false
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Manager hands out one breaker per backend server. A nil or disabled
// manager returns nil breakers, which callers treat as "always allow".
type Manager struct {
	mu      sync.Mutex
	cfg     Config
	enabled bool
	m       map[string]*Breaker
}

func NewManager(cfg Config, enabled bool) *Manager {
	return &Manager{cfg: cfg.withDefaults(), enabled: enabled, m: make(map[string]*Breaker)}
}

// Enabled reports whether the manager hands out live breakers.
func (m *Manager) Enabled() bool { return m != nil && m.enabled }

// Get returns the breaker for a server, creating it on first use. Returns
// nil when the manager is nil or disabled.
func (m *Manager) Get(server string) *Breaker {
	if m == nil || !m.enabled {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.m[server]
	if !ok {
		b = New(m.cfg)
		m.m[server] = b
	}
	return b
}
