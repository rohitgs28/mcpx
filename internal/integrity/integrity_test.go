package integrity

import (
	"encoding/json"
	"testing"
)

func tool(name, desc string) json.RawMessage {
	return json.RawMessage(`{"name":"` + name + `","description":"` + desc + `"}`)
}

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"warn":    ModeWarn,
		"enforce": ModeEnforce,
		"off":     ModeOff,
		"":        ModeOff,
		"bogus":   ModeOff,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreDisabled(t *testing.T) {
	s := NewStore(ModeOff)
	if s.Enabled() {
		t.Fatal("off store should not be enabled")
	}
	if v := s.Check("srv", []json.RawMessage{tool("a", "x")}); v != nil {
		t.Fatalf("disabled store should return no violations, got %v", v)
	}
}

func TestStoreFirstSeenNoViolation(t *testing.T) {
	s := NewStore(ModeWarn)
	v := s.Check("srv", []json.RawMessage{tool("a", "x"), tool("b", "y")})
	if len(v) != 0 {
		t.Fatalf("first sighting should record baselines, not violate; got %d violations", len(v))
	}
}

func TestStoreStableNoViolation(t *testing.T) {
	s := NewStore(ModeWarn)
	s.Check("srv", []json.RawMessage{tool("a", "x")})
	v := s.Check("srv", []json.RawMessage{tool("a", "x")})
	if len(v) != 0 {
		t.Fatalf("unchanged schema should not violate; got %v", v)
	}
}

func TestStoreDetectsMutation(t *testing.T) {
	s := NewStore(ModeWarn)
	s.Check("srv", []json.RawMessage{tool("a", "original")})
	v := s.Check("srv", []json.RawMessage{tool("a", "MUTATED")})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation for mutated tool, got %d", len(v))
	}
	if v[0].Tool != "a" || v[0].Server != "srv" {
		t.Errorf("unexpected violation target: %+v", v[0])
	}
	if v[0].OldHash == v[0].NewHash || v[0].OldHash == "" || v[0].NewHash == "" {
		t.Errorf("expected differing non-empty hashes, got old=%q new=%q", v[0].OldHash, v[0].NewHash)
	}
}

func TestStoreCanonicalizesKeyOrder(t *testing.T) {
	s := NewStore(ModeWarn)
	s.Check("srv", []json.RawMessage{json.RawMessage(`{"name":"a","description":"x"}`)})
	// Same content, different key order — must not be flagged as a mutation.
	v := s.Check("srv", []json.RawMessage{json.RawMessage(`{"description":"x","name":"a"}`)})
	if len(v) != 0 {
		t.Fatalf("key-order-only change must not violate; got %v", v)
	}
}

func TestStorePersistentMutationKeepsReporting(t *testing.T) {
	s := NewStore(ModeEnforce)
	s.Check("srv", []json.RawMessage{tool("a", "original")})
	if v := s.Check("srv", []json.RawMessage{tool("a", "MUTATED")}); len(v) != 1 {
		t.Fatalf("first mutated check: want 1 violation, got %d", len(v))
	}
	// Baseline is retained, so the same mutation is reported again rather than
	// silently accepted.
	if v := s.Check("srv", []json.RawMessage{tool("a", "MUTATED")}); len(v) != 1 {
		t.Fatalf("repeated mutated check: want 1 violation, got %d", len(v))
	}
}

func TestStorePerServerIsolation(t *testing.T) {
	s := NewStore(ModeWarn)
	s.Check("srv1", []json.RawMessage{tool("a", "x")})
	// Same tool name on a different server is a distinct pin (first sighting).
	if v := s.Check("srv2", []json.RawMessage{tool("a", "y")}); len(v) != 0 {
		t.Fatalf("different server should be a fresh baseline; got %v", v)
	}
}
