// Package cache stores backend responses for explicitly opted-in, idempotent
// MCP tools so repeated identical calls are served from memory instead of the
// upstream server.
//
// The design follows three rules that keep a cache from becoming a security
// hole:
//
//  1. Deny by default. Nothing is cached unless a tool is named in the
//     server's cache.tools config, with a TTL.
//  2. The client identity is part of the cache key. Two clients with
//     different per-client policies never share an entry, so a cached
//     response cannot leak across a tenant boundary.
//  3. Only successful, non-streaming, bounded-size JSON-RPC results are
//     stored. Errors (transport, JSON-RPC, or MCP isError results) are never
//     cached, so a blip cannot be pinned for the whole TTL.
//
// The store is a bounded LRU with per-entry TTL, plus single-flight: N
// concurrent misses on the same key make one backend call and share the
// result. Everything is in-memory and stdlib-only — no sidecar, no Redis,
// consistent with mcpx's single-binary design.
package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Defaults applied when the corresponding config value is zero.
const (
	DefaultMaxEntries   = 1000
	DefaultTTL          = 60 * time.Second
	DefaultMaxBodyBytes = 256 << 10 // 256 KiB
)

// Entry is a cached backend response. Body is the raw JSON-RPC response bytes
// exactly as the backend produced them; the caller rewrites the JSON-RPC id
// before serving it to a different request.
type Entry struct {
	Body        []byte
	ContentType string
	StoredAt    time.Time
	ExpiresAt   time.Time
}

// Age reports how long ago the entry was stored, relative to now.
func (e *Entry) Age(now time.Time) time.Duration { return now.Sub(e.StoredAt) }

// Stats is a point-in-time snapshot of store occupancy.
type Stats struct {
	Entries int
	MaxSize int
}

// Config tunes a Store. Zero values take the package defaults.
type Config struct {
	MaxEntries   int
	DefaultTTL   time.Duration
	MaxBodyBytes int
}

func (c Config) withDefaults() Config {
	if c.MaxEntries <= 0 {
		c.MaxEntries = DefaultMaxEntries
	}
	if c.DefaultTTL <= 0 {
		c.DefaultTTL = DefaultTTL
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}
	return c
}

// entry is the internal LRU node value.
type entry struct {
	key string
	val *Entry
}

// call tracks one in-flight backend fetch so concurrent misses on the same key
// can wait for it instead of stampeding the upstream.
type call struct {
	done chan struct{}
	val  *Entry // nil when the response turned out to be non-cacheable
	// waiting counts followers currently blocked on done. Guarded by
	// Store.flightMu.
	waiting int
}

// Store is a bounded, TTL'd LRU of backend responses with single-flight on
// misses. A nil *Store is a valid disabled cache: every method is a no-op, so
// callers need no nil checks beyond Enabled.
type Store struct {
	cfg     Config
	enabled bool

	mu    sync.Mutex
	ll    *list.List               // front = most recently used
	items map[string]*list.Element // key -> element holding *entry

	flightMu sync.Mutex
	flight   map[string]*call

	// now is swappable in tests to exercise expiry without sleeping.
	now func() time.Time

	// evictions counts entries dropped to stay under MaxEntries (not expiries).
	evictions int64
}

// New builds a store. A disabled store still answers Enabled/Stats so callers
// can wire it unconditionally.
func New(cfg Config, enabled bool) *Store {
	return &Store{
		cfg:     cfg.withDefaults(),
		enabled: enabled,
		ll:      list.New(),
		items:   make(map[string]*list.Element),
		flight:  make(map[string]*call),
		now:     time.Now,
	}
}

// Enabled reports whether the store caches anything.
func (s *Store) Enabled() bool { return s != nil && s.enabled }

// MaxBodyBytes is the largest response the store will accept.
func (s *Store) MaxBodyBytes() int {
	if s == nil {
		return 0
	}
	return s.cfg.MaxBodyBytes
}

// DefaultTTL is the TTL applied to tools configured without an explicit one.
func (s *Store) DefaultTTL() time.Duration {
	if s == nil {
		return 0
	}
	return s.cfg.DefaultTTL
}

// Get returns a live entry for key, or nil on miss. An expired entry is
// dropped and reported as a miss.
func (s *Store) Get(key string) *Entry {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.items[key]
	if !ok {
		return nil
	}
	e := el.Value.(*entry)
	if !s.now().Before(e.val.ExpiresAt) {
		s.removeElement(el)
		return nil
	}
	s.ll.MoveToFront(el)
	return e.val
}

// Put stores body under key for ttl. Bodies over MaxBodyBytes are dropped
// rather than stored, so one oversized result cannot evict the whole cache.
// The caller retains ownership of body; Put copies it.
func (s *Store) Put(key string, body []byte, contentType string, ttl time.Duration) *Entry {
	if !s.Enabled() || len(body) > s.cfg.MaxBodyBytes {
		return nil
	}
	if ttl <= 0 {
		ttl = s.cfg.DefaultTTL
	}
	now := s.now()
	cp := make([]byte, len(body))
	copy(cp, body)
	val := &Entry{Body: cp, ContentType: contentType, StoredAt: now, ExpiresAt: now.Add(ttl)}

	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		el.Value.(*entry).val = val
		s.ll.MoveToFront(el)
		return val
	}
	s.items[key] = s.ll.PushFront(&entry{key: key, val: val})
	for s.ll.Len() > s.cfg.MaxEntries {
		if oldest := s.ll.Back(); oldest != nil {
			s.removeElement(oldest)
			s.evictions++
		}
	}
	return val
}

// removeElement drops an element from both the list and the index.
// Callers must hold s.mu.
func (s *Store) removeElement(el *list.Element) {
	s.ll.Remove(el)
	delete(s.items, el.Value.(*entry).key)
}

// Purge empties the cache. Used when a reload should not serve results
// produced under the previous config.
func (s *Store) Purge() {
	if !s.Enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ll.Init()
	s.items = make(map[string]*list.Element)
}

// Stats returns current occupancy.
func (s *Store) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Entries: s.ll.Len(), MaxSize: s.cfg.MaxEntries}
}

// Evictions returns the number of entries dropped for capacity so far.
func (s *Store) Evictions() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evictions
}

// Lead claims the right to fetch key from the backend.
//
// The first caller for a key becomes the leader: lead is true and it must call
// Release exactly once with the resulting entry (or nil if the response turned
// out to be non-cacheable) — defer it, so a panicking handler cannot strand
// followers. Later callers block until the leader releases and receive its
// entry in shared; a nil shared means the leader produced nothing cacheable
// and the follower must make its own backend call.
//
// A follower whose ctx is cancelled (client gone, request timed out) stops
// waiting and returns (false, nil) rather than outliving its request.
//
// Single-flight only ever collapses work — a follower that gets nil is never
// wrong, just unlucky.
func (s *Store) Lead(ctx context.Context, key string) (lead bool, shared *Entry) {
	if !s.Enabled() {
		return true, nil
	}
	s.flightMu.Lock()
	c, ok := s.flight[key]
	if !ok {
		s.flight[key] = &call{done: make(chan struct{})}
		s.flightMu.Unlock()
		return true, nil
	}
	c.waiting++
	s.flightMu.Unlock()

	select {
	case <-c.done:
	case <-ctx.Done():
	}

	s.flightMu.Lock()
	c.waiting--
	s.flightMu.Unlock()
	// c.val is written before done is closed, so reading it after the receive
	// is safe; on a cancelled ctx there is nothing to hand back.
	select {
	case <-c.done:
		return false, c.val
	default:
		return false, nil
	}
}

// waiters reports how many followers are blocked on key. Used by tests to
// synchronize on the single-flight state without sleeping.
func (s *Store) waiters(key string) int {
	s.flightMu.Lock()
	defer s.flightMu.Unlock()
	if c, ok := s.flight[key]; ok {
		return c.waiting
	}
	return 0
}

// Release completes the in-flight fetch for key, handing val to any waiters.
// It must be called by the leader (and only the leader), exactly once —
// defer it, so a panicking handler cannot strand waiters forever.
func (s *Store) Release(key string, val *Entry) {
	if !s.Enabled() {
		return
	}
	s.flightMu.Lock()
	c, ok := s.flight[key]
	if ok {
		delete(s.flight, key)
	}
	s.flightMu.Unlock()
	if !ok {
		return
	}
	c.val = val
	close(c.done)
}

// Key builds the cache key for one tool call: server, tool, client identity,
// and a canonical rendering of the arguments. Hashing keeps keys short and
// bounded regardless of argument size, and keeps argument values (which may be
// sensitive) out of memory-dumped map keys.
//
// Arguments are canonicalized rather than hashed as raw JSON so that two calls
// differing only in key order or whitespace share an entry.
func Key(server, tool, client string, args map[string]any) string {
	h := sha256.New()
	writeField := func(s string) {
		// Length-prefix every field so ("ab","c") and ("a","bc") differ.
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte(":"))
		h.Write([]byte(s))
	}
	writeField(server)
	writeField(tool)
	writeField(client)
	writeField(canonical(args))
	return hex.EncodeToString(h.Sum(nil))
}

// canonical renders args as JSON with object keys sorted at every level, so
// semantically identical argument sets produce identical bytes.
func canonical(v any) string {
	b, err := json.Marshal(sortValue(v))
	if err != nil {
		return "\x00unencodable"
	}
	return string(b)
}

// sortValue rewrites maps as sorted key/value pair slices, recursively.
// Encoding pairs (rather than relying on encoding/json's own map-key sorting)
// keeps the transformation explicit and applies to nested values too.
func sortValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]any, 0, len(keys)*2)
		for _, k := range keys {
			pairs = append(pairs, k, sortValue(t[k]))
		}
		return pairs
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = sortValue(e)
		}
		return out
	default:
		return v
	}
}
