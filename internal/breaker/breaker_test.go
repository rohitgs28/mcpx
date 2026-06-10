package breaker

import (
	"testing"
	"time"
)

// fakeClock lets tests advance time deterministically.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestBreaker(cfg Config) (*Breaker, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := New(cfg)
	b.now = clk.now
	return b, clk
}

func TestBreaker_OpensAtThreshold(t *testing.T) {
	b, _ := newTestBreaker(Config{FailureThreshold: 3, Cooldown: 30 * time.Second})

	for i := 0; i < 2; i++ {
		if tripped := b.RecordFailure(); tripped {
			t.Fatalf("tripped after %d failures, threshold is 3", i+1)
		}
		if !b.Allow() {
			t.Fatalf("should still allow after %d failures", i+1)
		}
	}
	if tripped := b.RecordFailure(); !tripped {
		t.Fatal("third failure should trip the breaker")
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}
	if b.Allow() {
		t.Fatal("open breaker must not allow requests")
	}
}

func TestBreaker_SuccessResetsStreak(t *testing.T) {
	b, _ := newTestBreaker(Config{FailureThreshold: 3})
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Fatal("non-consecutive failures must not open the breaker")
	}
}

func TestBreaker_HalfOpenAfterCooldown(t *testing.T) {
	b, clk := newTestBreaker(Config{FailureThreshold: 1, Cooldown: 30 * time.Second, HalfOpenMax: 1})
	b.RecordFailure()

	clk.advance(29 * time.Second)
	if b.Allow() {
		t.Fatal("breaker must stay open before cooldown elapses")
	}
	clk.advance(2 * time.Second)
	if !b.Allow() {
		t.Fatal("first request after cooldown should be admitted as a probe")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	// Only HalfOpenMax probes are admitted concurrently.
	if b.Allow() {
		t.Fatal("second concurrent probe must be rejected (half_open_max=1)")
	}
}

func TestBreaker_ProbeSuccessCloses(t *testing.T) {
	b, clk := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Second})
	b.RecordFailure()
	clk.advance(2 * time.Second)
	if !b.Allow() {
		t.Fatal("probe should be admitted")
	}
	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed after successful probe", b.State())
	}
	if !b.Allow() {
		t.Fatal("closed breaker must allow")
	}
}

func TestBreaker_ProbeFailureReopens(t *testing.T) {
	b, clk := newTestBreaker(Config{FailureThreshold: 1, Cooldown: 10 * time.Second})
	b.RecordFailure()
	clk.advance(11 * time.Second)
	b.Allow() // half-open probe
	if tripped := b.RecordFailure(); !tripped {
		t.Fatal("failed probe should report a trip")
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open after failed probe", b.State())
	}
	// Cooldown restarts from the failed probe.
	clk.advance(9 * time.Second)
	if b.Allow() {
		t.Fatal("cooldown must restart after a failed probe")
	}
	clk.advance(2 * time.Second)
	if !b.Allow() {
		t.Fatal("next probe should be admitted after the restarted cooldown")
	}
}

func TestManager_Disabled(t *testing.T) {
	if m := NewManager(Config{}, false); m.Get("any") != nil {
		t.Fatal("disabled manager must return nil breakers")
	}
	var nilM *Manager
	if nilM.Get("any") != nil {
		t.Fatal("nil manager must return nil breakers")
	}
}

func TestManager_PerServerIsolation(t *testing.T) {
	m := NewManager(Config{FailureThreshold: 1}, true)
	m.Get("a").RecordFailure()
	if m.Get("a").State() != StateOpen {
		t.Fatal("server a should be open")
	}
	if m.Get("b").State() != StateClosed {
		t.Fatal("server b must be unaffected by server a's failures")
	}
	if m.Get("a") != m.Get("a") {
		t.Fatal("Get must return the same breaker for the same server")
	}
}

func TestConfigDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.FailureThreshold != 5 || c.Cooldown != 30*time.Second || c.HalfOpenMax != 1 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}
