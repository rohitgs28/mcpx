package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock lets tests expire entries without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestStore(cfg Config) (*Store, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s := New(cfg, true)
	s.now = clk.now
	return s, clk
}

func TestStore_StoresAndServes(t *testing.T) {
	s, _ := newTestStore(Config{})

	if got := s.Get("k"); got != nil {
		t.Fatalf("expected miss on empty store, got %q", got.Body)
	}
	s.Put("k", []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`), "application/json", time.Minute)

	got := s.Get("k")
	if got == nil {
		t.Fatal("expected hit after Put")
	}
	if string(got.Body) != `{"jsonrpc":"2.0","id":1,"result":{}}` {
		t.Fatalf("body round-trip mismatch: %s", got.Body)
	}
	if got.ContentType != "application/json" {
		t.Fatalf("content type = %q", got.ContentType)
	}
}

func TestStore_PutCopiesBody(t *testing.T) {
	s, _ := newTestStore(Config{})
	body := []byte(`{"result":1}`)
	s.Put("k", body, "application/json", time.Minute)

	// A caller reusing its buffer must not corrupt the cached entry.
	copy(body, []byte(`{"result":9}`))

	if got := s.Get("k"); string(got.Body) != `{"result":1}` {
		t.Fatalf("cached body aliased the caller's buffer: %s", got.Body)
	}
}

func TestStore_ExpiredEntryIsAMiss(t *testing.T) {
	s, clk := newTestStore(Config{})
	s.Put("k", []byte(`{"result":1}`), "application/json", 30*time.Second)

	clk.advance(29 * time.Second)
	if s.Get("k") == nil {
		t.Fatal("entry expired early")
	}
	clk.advance(2 * time.Second)
	if s.Get("k") != nil {
		t.Fatal("expected miss after TTL elapsed")
	}
	if n := s.Stats().Entries; n != 0 {
		t.Fatalf("expired entry should be dropped on read, entries = %d", n)
	}
}

func TestStore_ZeroTTLUsesDefault(t *testing.T) {
	s, clk := newTestStore(Config{DefaultTTL: 10 * time.Second})
	s.Put("k", []byte(`{"result":1}`), "application/json", 0)

	clk.advance(9 * time.Second)
	if s.Get("k") == nil {
		t.Fatal("entry should still be fresh under the default TTL")
	}
	clk.advance(2 * time.Second)
	if s.Get("k") != nil {
		t.Fatal("entry should have expired at the default TTL")
	}
}

func TestStore_EvictsLeastRecentlyUsed(t *testing.T) {
	s, _ := newTestStore(Config{MaxEntries: 2})
	s.Put("a", []byte(`{"result":"a"}`), "", time.Minute)
	s.Put("b", []byte(`{"result":"b"}`), "", time.Minute)

	// Touch "a" so "b" becomes the least recently used.
	if s.Get("a") == nil {
		t.Fatal("a should be cached")
	}
	s.Put("c", []byte(`{"result":"c"}`), "", time.Minute)

	if s.Get("b") != nil {
		t.Fatal("b was least recently used and should have been evicted")
	}
	if s.Get("a") == nil || s.Get("c") == nil {
		t.Fatal("a and c should both be resident")
	}
	if n := s.Stats().Entries; n != 2 {
		t.Fatalf("entries = %d, want 2 (max_entries)", n)
	}
	if e := s.Evictions(); e != 1 {
		t.Fatalf("evictions = %d, want 1", e)
	}
}

func TestStore_OverwriteKeepsOneEntry(t *testing.T) {
	s, _ := newTestStore(Config{MaxEntries: 2})
	s.Put("k", []byte(`{"result":1}`), "", time.Minute)
	s.Put("k", []byte(`{"result":2}`), "", time.Minute)

	if n := s.Stats().Entries; n != 1 {
		t.Fatalf("entries = %d, want 1", n)
	}
	if got := s.Get("k"); string(got.Body) != `{"result":2}` {
		t.Fatalf("overwrite did not replace the body: %s", got.Body)
	}
}

func TestStore_RejectsOversizedBody(t *testing.T) {
	s, _ := newTestStore(Config{MaxBodyBytes: 8})
	if e := s.Put("k", []byte(`{"result":"way too long"}`), "", time.Minute); e != nil {
		t.Fatal("oversized body should not be stored")
	}
	if s.Get("k") != nil {
		t.Fatal("oversized body should not be retrievable")
	}
}

func TestStore_Purge(t *testing.T) {
	s, _ := newTestStore(Config{})
	s.Put("a", []byte(`{"result":1}`), "", time.Minute)
	s.Put("b", []byte(`{"result":2}`), "", time.Minute)
	s.Purge()

	if n := s.Stats().Entries; n != 0 {
		t.Fatalf("entries after purge = %d, want 0", n)
	}
	if s.Get("a") != nil {
		t.Fatal("purged entry still served")
	}
}

func TestStore_DisabledIsInert(t *testing.T) {
	for name, s := range map[string]*Store{
		"nil":      nil,
		"disabled": New(Config{}, false),
	} {
		t.Run(name, func(t *testing.T) {
			if s.Enabled() {
				t.Fatal("store should report disabled")
			}
			if e := s.Put("k", []byte(`{"result":1}`), "", time.Minute); e != nil {
				t.Fatal("disabled store should not store")
			}
			if s.Get("k") != nil {
				t.Fatal("disabled store should never hit")
			}
			// A disabled store must still let every caller through as its own
			// leader, or requests would deadlock waiting on nothing.
			lead, shared := s.Lead(context.Background(), "k")
			if !lead || shared != nil {
				t.Fatalf("Lead on disabled store = (%v, %v), want (true, nil)", lead, shared)
			}
			s.Release("k", nil)
			s.Purge()
			if got := s.Stats(); got.Entries != 0 {
				t.Fatalf("stats on disabled store = %+v", got)
			}
		})
	}
}

// waitForWaiters blocks until n followers are parked on key, so tests can act
// at a known point in the single-flight lifecycle instead of guessing.
func waitForWaiters(t *testing.T, s *Store, key string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.waiters(key) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d followers on %q (have %d)", n, key, s.waiters(key))
}

func TestStore_SingleFlightSharesTheLeadersEntry(t *testing.T) {
	s, _ := newTestStore(Config{})

	lead, shared := s.Lead(context.Background(), "k")
	if !lead || shared != nil {
		t.Fatalf("first caller should lead, got (%v, %v)", lead, shared)
	}

	const followers = 8
	var wg sync.WaitGroup
	results := make([]*Entry, followers)
	for i := 0; i < followers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			isLead, got := s.Lead(context.Background(), "k")
			if isLead {
				t.Errorf("follower %d became a leader while a fetch was in flight", i)
			}
			results[i] = got
		}(i)
	}
	waitForWaiters(t, s, "k", followers)

	stored := s.Put("k", []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`), "application/json", time.Minute)
	s.Release("k", stored)
	wg.Wait()

	for i, got := range results {
		if got == nil {
			t.Fatalf("follower %d got no shared entry", i)
		}
		if string(got.Body) != string(stored.Body) {
			t.Fatalf("follower %d body = %s, want %s", i, got.Body, stored.Body)
		}
	}
}

func TestStore_SingleFlightFollowersProceedWhenNothingCached(t *testing.T) {
	s, _ := newTestStore(Config{})
	lead, _ := s.Lead(context.Background(), "k")
	if !lead {
		t.Fatal("first caller should lead")
	}

	done := make(chan *Entry)
	go func() {
		_, shared := s.Lead(context.Background(), "k")
		done <- shared
	}()
	waitForWaiters(t, s, "k", 1)

	// The leader's response turned out to be non-cacheable.
	s.Release("k", nil)

	if got := <-done; got != nil {
		t.Fatalf("follower should get nil and fetch for itself, got %s", got.Body)
	}
	// The key is free again: the next caller leads.
	if lead, _ := s.Lead(context.Background(), "k"); !lead {
		t.Fatal("key should be claimable again after Release")
	}
}

func TestStore_FollowerStopsWaitingWhenItsRequestIsCancelled(t *testing.T) {
	s, _ := newTestStore(Config{})
	if lead, _ := s.Lead(context.Background(), "k"); !lead {
		t.Fatal("first caller should lead")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *Entry, 1)
	go func() {
		_, shared := s.Lead(ctx, "k")
		done <- shared
	}()
	waitForWaiters(t, s, "k", 1)

	cancel() // the client hung up while its request was parked

	select {
	case got := <-done:
		if got != nil {
			t.Fatalf("cancelled follower should get nothing, got %s", got.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled follower is still waiting on the leader")
	}
	s.Release("k", nil)
}

func TestStore_ReleaseWithoutLeadIsSafe(t *testing.T) {
	s, _ := newTestStore(Config{})
	s.Release("never-claimed", nil) // must not panic or block
}

func TestKey_IgnoresArgumentOrder(t *testing.T) {
	a := map[string]any{"path": "/tmp", "depth": 2.0, "opts": map[string]any{"x": 1.0, "y": 2.0}}
	b := map[string]any{"opts": map[string]any{"y": 2.0, "x": 1.0}, "depth": 2.0, "path": "/tmp"}

	if Key("s", "t", "c", a) != Key("s", "t", "c", b) {
		t.Fatal("argument order changed the cache key")
	}
}

func TestKey_SeparatesDistinctCalls(t *testing.T) {
	base := Key("srv", "tool", "client", map[string]any{"p": "1"})
	cases := map[string]string{
		"different server":   Key("srv2", "tool", "client", map[string]any{"p": "1"}),
		"different tool":     Key("srv", "tool2", "client", map[string]any{"p": "1"}),
		"different client":   Key("srv", "tool", "client2", map[string]any{"p": "1"}),
		"different args":     Key("srv", "tool", "client", map[string]any{"p": "2"}),
		"extra arg":          Key("srv", "tool", "client", map[string]any{"p": "1", "q": "1"}),
		"no args":            Key("srv", "tool", "client", nil),
		"field boundary":     Key("sr", "vtool", "client", map[string]any{"p": "1"}),
		"nested vs flat arg": Key("srv", "tool", "client", map[string]any{"p": map[string]any{"1": nil}}),
	}
	for name, k := range cases {
		if k == base {
			t.Errorf("%s produced the same key as the base call", name)
		}
	}
}

func TestKey_IsStable(t *testing.T) {
	// Distinct maps with equal contents must hash identically, so a repeat
	// call with freshly-decoded arguments finds the entry the first one left.
	first := Key("s", "t", "c", map[string]any{"q": "hello", "n": 3.0})
	second := Key("s", "t", "c", map[string]any{"q": "hello", "n": 3.0})
	if first != second {
		t.Fatalf("key is not deterministic: %s != %s", first, second)
	}
}

func TestCacheable(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"success result", `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`, true},
		{"empty object result", `{"jsonrpc":"2.0","id":1,"result":{}}`, true},
		{"scalar result", `{"jsonrpc":"2.0","id":1,"result":42}`, true},
		{"explicit isError false", `{"jsonrpc":"2.0","id":1,"result":{"isError":false}}`, true},
		{"jsonrpc error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`, false},
		{"tool reported failure", `{"jsonrpc":"2.0","id":1,"result":{"isError":true}}`, false},
		{"no result", `{"jsonrpc":"2.0","id":1}`, false},
		{"malformed", `{"jsonrpc":`, false},
		{"batch", `[{"jsonrpc":"2.0","id":1,"result":{}}]`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Cacheable([]byte(tc.body)); got != tc.want {
				t.Fatalf("Cacheable(%s) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestRetargetID(t *testing.T) {
	cached := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hi"}]}}`)

	out := RetargetID(cached, json.RawMessage(`77`))
	var got struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("retargeted body does not parse: %v (%s)", err, out)
	}
	if string(got.ID) != "77" {
		t.Fatalf("id = %s, want 77", got.ID)
	}
	if string(got.Result) != `{"content":[{"type":"text","text":"hi"}]}` {
		t.Fatalf("result was altered: %s", got.Result)
	}
}

func TestRetargetID_PreservesLargeAndStringIDs(t *testing.T) {
	// A float64 round-trip would corrupt an id this large.
	for _, id := range []string{`9007199254740993`, `"req-abc"`, `null`} {
		out := RetargetID([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`), json.RawMessage(id))
		var got struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("id %s: %v", id, err)
		}
		if string(got.ID) != id {
			t.Fatalf("id = %s, want %s", got.ID, id)
		}
	}
}

func TestRetargetID_LeavesBodyAloneWhenItCannot(t *testing.T) {
	same := []byte(`{"jsonrpc":"2.0","id":5,"result":{}}`)
	if got := RetargetID(same, json.RawMessage(`5`)); string(got) != string(same) {
		t.Fatalf("matching id should return the original bytes, got %s", got)
	}
	broken := []byte(`not json`)
	if got := RetargetID(broken, json.RawMessage(`1`)); string(got) != string(broken) {
		t.Fatalf("unparseable body should pass through, got %s", got)
	}
	if got := RetargetID(same, nil); string(got) != string(same) {
		t.Fatalf("empty id should pass through, got %s", got)
	}
}

func TestStore_ConcurrentAccessIsRaceFree(t *testing.T) {
	s := New(Config{MaxEntries: 16}, true)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i%8)
			s.Put(key, []byte(`{"result":1}`), "application/json", time.Minute)
			s.Get(key)
			s.Stats()
			s.Evictions()
		}(i)
	}
	wg.Wait()
}
