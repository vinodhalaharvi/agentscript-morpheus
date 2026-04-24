// Package blackboard implements a shared state primitive for multi-agent
// coordination. Agents write entries; subscribers receive events; an
// equilibrium detector tracks write activity over a sliding window to
// determine when the board has stabilized.
//
// Concurrency model:
//
//   - One goroutine per agent (callers' responsibility)
//   - Board operations are safe under concurrent access via sync.RWMutex
//   - Subscription fan-out happens synchronously within Write, so an
//     expensive handler can slow writes. For v1 this is acceptable;
//     callers can use goroutines inside handlers if they need async
//     dispatch.
//   - All pattern-matching filtering happens at subscribe-time compile,
//     at dispatch-time match. See pkg/matching.
package blackboard

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vinodhalaharvi/agentscript/pkg/matching"
)

// Entry is a single write to the board. Keys are opaque strings (callers
// define the convention — often path-like: "cells/3,5" or "beliefs/market_direction").
type Entry struct {
	Key       string
	Value     matching.Value // typically a map[string]interface{} from JSON
	By        string         // agent id that wrote this
	WrittenAt time.Time
	Round     int // logical round in which this was written
}

// WriteEvent is dispatched to subscribers on every successful write.
// It carries the new entry plus the previous value (nil if this is the
// first write at that key). Subscribers can match on the entry value
// via patterns.
type WriteEvent struct {
	Entry
	Previous matching.Value // nil if new key
}

// WritePolicy controls what happens when a write targets a key that
// already has a value.
type WritePolicy int

const (
	// LastWriteWins (default): the new write always replaces.
	LastWriteWins WritePolicy = iota

	// HigherConfidenceWins: the new write replaces only if its
	// {confidence: float} field is strictly greater than the existing
	// entry's {confidence} field. Requires both entries to be objects
	// with a numeric "confidence" field. If the existing entry lacks
	// the field, new write wins. If the new write lacks it, write is
	// rejected silently.
	HigherConfidenceWins

	// AppendOnly: writes to an existing key are rejected. Useful for
	// append-only logs where re-writes indicate a bug.
	AppendOnly
)

// Subscription is a registration for write events matching a pattern.
// Handler is called synchronously on each match. If the pattern doesn't
// match the event's entry value, Handler is not invoked.
type Subscription struct {
	id      uint64
	owner   string // agent id (for debugging)
	pattern matching.Pattern
	// Optional key predicate — if set, only events on keys satisfying
	// this function trigger the match. Used to express patterns like
	// "only cells/*" without encoding that in the value pattern.
	keyFilter func(string) bool
	handler   func(WriteEvent, matching.Bindings) error
}

// Board is the shared state substrate.
type Board struct {
	mu sync.RWMutex

	entries   map[string]Entry
	subs      map[uint64]*Subscription
	nextSubID uint64

	policy WritePolicy

	// Equilibrium tracking
	round          int // incremented by NextRound()
	lastWriteRound int // the round number of the most recent write

	// writeCount is incremented on every successful write. Tests and
	// diagnostics can read this to verify activity.
	writeCount atomic.Uint64
}

// NewBoard creates a fresh blackboard with the given write policy.
func NewBoard(policy WritePolicy) *Board {
	return &Board{
		entries: make(map[string]Entry),
		subs:    make(map[uint64]*Subscription),
		policy:  policy,
	}
}

// Write attempts to write an entry. Returns (wrote, error). wrote is
// false when the write policy rejected the write (not an error — a
// legitimate outcome under HigherConfidenceWins and AppendOnly).
//
// On a successful write, subscribers are notified synchronously.
func (b *Board) Write(key string, value matching.Value, by string) (bool, error) {
	b.mu.Lock()

	prev, exists := b.entries[key]
	// Apply write policy
	switch b.policy {
	case HigherConfidenceWins:
		if exists {
			existingC := objectFloat(prev.Value, "confidence")
			newC := objectFloat(value, "confidence")
			if newC == nil {
				b.mu.Unlock()
				return false, nil // silently reject — new value has no confidence
			}
			if existingC != nil && *newC <= *existingC {
				b.mu.Unlock()
				return false, nil
			}
		}
	case AppendOnly:
		if exists {
			b.mu.Unlock()
			return false, fmt.Errorf("append-only violation: key %q already exists", key)
		}
	case LastWriteWins:
		// no-op
	}

	newEntry := Entry{
		Key:       key,
		Value:     value,
		By:        by,
		WrittenAt: time.Now(),
		Round:     b.round,
	}
	b.entries[key] = newEntry
	b.lastWriteRound = b.round
	b.writeCount.Add(1)

	// Snapshot subscribers so we can notify without holding the lock
	subs := make([]*Subscription, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	// Dispatch outside the lock to prevent subscriber handlers from
	// deadlocking if they call Read/Write.
	event := WriteEvent{Entry: newEntry}
	if exists {
		event.Previous = prev.Value
	}
	b.dispatch(subs, event)
	return true, nil
}

// NotifyTick dispatches an event to all subscribers WITHOUT counting as
// a board write. The coordination engine uses this to wake agents each
// round so they can volunteer contributions. Because this doesn't touch
// writeCount or lastWriteRound, the equilibrium predicate continues to
// measure real agent activity rather than engine plumbing.
//
// The tick key and value are still visible to subscribers as a normal
// WriteEvent — agents subscribing to "__tick__/*" will receive these.
func (b *Board) NotifyTick(key string, value matching.Value) {
	b.mu.RLock()
	subs := make([]*Subscription, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	round := b.round
	b.mu.RUnlock()

	event := WriteEvent{
		Entry: Entry{
			Key:       key,
			Value:     value,
			By:        "__engine__",
			WrittenAt: time.Now(),
			Round:     round,
		},
	}
	b.dispatch(subs, event)
}

func (b *Board) dispatch(subs []*Subscription, event WriteEvent) {
	for _, s := range subs {
		if s.keyFilter != nil && !s.keyFilter(event.Key) {
			continue
		}
		bindings, ok, err := matching.Match(s.pattern, event.Value)
		if err != nil {
			// Guard errors — log to stderr via the handler if it wants,
			// but don't break other subscribers
			continue
		}
		if !ok {
			continue
		}
		// Synchronous handler invocation; handler errors are swallowed
		// for now. Callers who care can track per-handler errors.
		_ = s.handler(event, bindings)
	}
}

// Read returns the current value for a key and whether it exists.
func (b *Board) Read(key string) (Entry, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	e, ok := b.entries[key]
	return e, ok
}

// Snapshot returns a copy of all entries. Snapshots are sorted by key
// for determinism in tests and witness blocks.
func (b *Board) Snapshot() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Entry, 0, len(b.entries))
	for _, e := range b.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Subscribe registers a handler for write events matching the given
// pattern. Optionally provide a keyFilter to pre-filter by key before
// pattern matching.
func (b *Board) Subscribe(owner string, pattern matching.Pattern, keyFilter func(string) bool, handler func(WriteEvent, matching.Bindings) error) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSubID++
	id := b.nextSubID
	b.subs[id] = &Subscription{
		id:        id,
		owner:     owner,
		pattern:   pattern,
		keyFilter: keyFilter,
		handler:   handler,
	}
	return id
}

// Unsubscribe removes a subscription by ID.
func (b *Board) Unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, id)
}

// NextRound advances the logical round counter. Called by the engine
// at each coordination round. Equilibrium predicates rely on this to
// measure stability windows.
func (b *Board) NextRound() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.round++
	return b.round
}

// CurrentRound returns the current logical round.
func (b *Board) CurrentRound() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.round
}

// LastWriteRound returns the round in which the most recent write
// occurred, or -1 if no writes have occurred.
func (b *Board) LastWriteRound() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.writeCount.Load() == 0 {
		return -1
	}
	return b.lastWriteRound
}

// WriteCount returns the total number of successful writes.
func (b *Board) WriteCount() uint64 {
	return b.writeCount.Load()
}

// ================================================================
// Convergence predicate: state equilibrium
// ================================================================

// NoWritesForRounds returns true if no writes have occurred in the last
// n rounds. At least n rounds must have elapsed total.
func (b *Board) NoWritesForRounds(n int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.round < n {
		return false
	}
	if b.writeCount.Load() == 0 {
		// No writes at all — are we past n rounds?
		return b.round >= n
	}
	return (b.round - b.lastWriteRound) >= n
}

// objectFloat reads value[key] as a float64 and returns a pointer, or
// nil if the value isn't an object or the field is missing or non-numeric.
// Used for HigherConfidenceWins policy — a tiny helper to avoid the
// switch-case clutter inside Write.
func objectFloat(v matching.Value, key string) *float64 {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	raw, present := m[key]
	if !present {
		return nil
	}
	switch n := raw.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	}
	return nil
}
