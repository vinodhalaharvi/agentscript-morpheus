package blackboard

import (
	"fmt"
	"sync"
	"testing"

	"github.com/vinodhalaharvi/agentscript/pkg/matching"
)

func TestBoardWriteAndRead(t *testing.T) {
	b := NewBoard(LastWriteWins)
	wrote, err := b.Write("cells/3,5", "A", "vocab-expert")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !wrote {
		t.Error("expected write to succeed")
	}
	e, ok := b.Read("cells/3,5")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if e.Value != "A" {
		t.Errorf("value: got %v want A", e.Value)
	}
	if e.By != "vocab-expert" {
		t.Errorf("by: got %v", e.By)
	}
}

func TestWritePolicyLastWriteWins(t *testing.T) {
	b := NewBoard(LastWriteWins)
	b.Write("k", "first", "a")
	wrote, _ := b.Write("k", "second", "b")
	if !wrote {
		t.Error("last-write-wins should accept second write")
	}
	e, _ := b.Read("k")
	if e.Value != "second" {
		t.Errorf("got %v want second", e.Value)
	}
}

func TestWritePolicyHigherConfidenceWins(t *testing.T) {
	b := NewBoard(HigherConfidenceWins)

	// First write with confidence 0.6
	v1 := map[string]interface{}{"letter": "A", "confidence": 0.6}
	b.Write("cell/1", v1, "a")

	// Second write with LOWER confidence — should be rejected
	v2 := map[string]interface{}{"letter": "B", "confidence": 0.4}
	wrote, _ := b.Write("cell/1", v2, "b")
	if wrote {
		t.Error("lower confidence should not win")
	}
	e, _ := b.Read("cell/1")
	if e.Value.(map[string]interface{})["letter"] != "A" {
		t.Error("letter should still be A")
	}

	// Third write with HIGHER confidence — should win
	v3 := map[string]interface{}{"letter": "C", "confidence": 0.8}
	wrote, _ = b.Write("cell/1", v3, "c")
	if !wrote {
		t.Error("higher confidence should win")
	}
	e, _ = b.Read("cell/1")
	if e.Value.(map[string]interface{})["letter"] != "C" {
		t.Error("letter should now be C")
	}
}

func TestWritePolicyAppendOnly(t *testing.T) {
	b := NewBoard(AppendOnly)
	b.Write("k", "first", "a")
	wrote, err := b.Write("k", "second", "b")
	if wrote {
		t.Error("append-only should reject re-writes")
	}
	if err == nil {
		t.Error("expected error on append-only violation")
	}
}

func TestSubscriptionFires(t *testing.T) {
	b := NewBoard(LastWriteWins)
	pat, _ := matching.Parse(`{status: "ready"}`)

	var got []string
	var mu sync.Mutex

	b.Subscribe("watcher", pat, nil, func(ev WriteEvent, bindings matching.Bindings) error {
		mu.Lock()
		got = append(got, ev.Key)
		mu.Unlock()
		return nil
	})

	// Write matching status
	b.Write("task/1", map[string]interface{}{"status": "ready"}, "worker")
	// Write non-matching
	b.Write("task/2", map[string]interface{}{"status": "pending"}, "worker")
	// Write matching
	b.Write("task/3", map[string]interface{}{"status": "ready"}, "worker")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(got), got)
	}
	if len(got) >= 1 && got[0] != "task/1" {
		t.Errorf("first match: %v", got[0])
	}
	if len(got) >= 2 && got[1] != "task/3" {
		t.Errorf("second match: %v", got[1])
	}
}

func TestSubscriptionKeyFilter(t *testing.T) {
	b := NewBoard(LastWriteWins)
	pat, _ := matching.Parse(`_`)

	var got []string
	b.Subscribe("cells-only", pat,
		func(k string) bool { return len(k) >= 6 && k[:6] == "cells/" },
		func(ev WriteEvent, _ matching.Bindings) error {
			got = append(got, ev.Key)
			return nil
		})

	b.Write("cells/1", "X", "a")
	b.Write("other/2", "Y", "a")
	b.Write("cells/3", "Z", "a")

	if len(got) != 2 {
		t.Fatalf("expected 2 cells matches, got %d: %v", len(got), got)
	}
}

func TestSubscriptionBindings(t *testing.T) {
	b := NewBoard(LastWriteWins)
	pat, _ := matching.Parse(`{value: $v, confidence: $c}`)

	var gotV, gotC matching.Value
	b.Subscribe("reader", pat, nil, func(ev WriteEvent, bindings matching.Bindings) error {
		gotV = bindings["v"]
		gotC = bindings["c"]
		return nil
	})

	b.Write("k", map[string]interface{}{"value": "hello", "confidence": 0.9}, "a")

	if gotV != "hello" {
		t.Errorf("v binding: got %v want hello", gotV)
	}
	if gotC != 0.9 {
		t.Errorf("c binding: got %v want 0.9", gotC)
	}
}

func TestRoundAdvancementAndEquilibrium(t *testing.T) {
	b := NewBoard(LastWriteWins)

	// Initially no rounds, no writes
	if b.NoWritesForRounds(3) {
		t.Error("should not be at equilibrium with 0 rounds elapsed")
	}

	// Round 1: write something
	b.NextRound()
	b.Write("k", "v", "a")

	// Round 2: no write
	b.NextRound()
	if b.NoWritesForRounds(2) {
		t.Error("only 1 round since last write, not 2")
	}

	// Round 3: no write. Now 2 rounds since last write.
	b.NextRound()
	if !b.NoWritesForRounds(2) {
		t.Error("should be at 2-round equilibrium")
	}
}

func TestNotifyTickDispatchesButDoesNotCount(t *testing.T) {
	// NotifyTick should wake subscribers without counting as a write.
	// This is critical for equilibrium detection: if engine ticks counted
	// as writes, the board would always appear active and equilibrium
	// would never fire.
	b := NewBoard(LastWriteWins)
	pat, _ := matching.Parse("_")

	var fired int
	b.Subscribe("watcher", pat, nil, func(ev WriteEvent, _ matching.Bindings) error {
		fired++
		return nil
	})

	// Advance some rounds and send ticks
	for i := 0; i < 5; i++ {
		b.NextRound()
		b.NotifyTick("__tick__", map[string]interface{}{"round": float64(i)})
	}

	// Subscriber should have fired 5 times (once per tick)
	if fired != 5 {
		t.Errorf("subscriber fired %d times, want 5", fired)
	}

	// But board should report zero writes
	if b.WriteCount() != 0 {
		t.Errorf("WriteCount: got %d, want 0 (ticks should not count)", b.WriteCount())
	}

	// Equilibrium should fire — no writes for 3 rounds means stable
	if !b.NoWritesForRounds(3) {
		t.Error("equilibrium should be satisfied — ticks alone should not block it")
	}
}

func TestNotifyTickVsWriteEquilibrium(t *testing.T) {
	// Real agent write resets the equilibrium clock; tick does not.
	b := NewBoard(LastWriteWins)

	// Round 1: real write
	b.NextRound()
	b.Write("agent-result", "hello", "agent-a")

	// Rounds 2-4: just ticks
	for i := 0; i < 3; i++ {
		b.NextRound()
		b.NotifyTick("__tick__", nil)
	}

	// Board should report 3 rounds since last REAL write
	if !b.NoWritesForRounds(3) {
		t.Error("should be at 3-round equilibrium (ticks don't reset clock)")
	}
}

func TestIdempotentWritesDontBlockEquilibrium(t *testing.T) {
	// A chatty agent that writes the same value every round should NOT
	// block equilibrium. The state isn't changing, so the system IS at
	// equilibrium even if the agent keeps re-asserting the same fact.
	// Critical: agents with helpful-ack behavior (writing the same thing
	// every tick) shouldn't prevent convergence.
	b := NewBoard(LastWriteWins)

	b.NextRound()
	b.Write("greeting", "hello", "agent-a")

	for i := 0; i < 3; i++ {
		b.NextRound()
		b.Write("greeting", "hello", "agent-a")
	}

	if b.WriteCount() != 4 {
		t.Errorf("WriteCount: got %d, want 4", b.WriteCount())
	}
	if b.LastWriteRound() != 1 {
		t.Errorf("LastWriteRound: got %d, want 1 (idempotent writes don't update)",
			b.LastWriteRound())
	}
	if !b.NoWritesForRounds(3) {
		t.Error("should be at 3-round equilibrium (idempotent writes don't count)")
	}
}

func TestDifferentValueResetsEquilibrium(t *testing.T) {
	b := NewBoard(LastWriteWins)

	b.NextRound()
	b.Write("k", "v1", "a")

	b.NextRound()
	b.NextRound()
	if !b.NoWritesForRounds(2) {
		t.Error("expected equilibrium after 2 quiet rounds")
	}

	b.NextRound()
	b.Write("k", "v2", "a")
	if b.NoWritesForRounds(2) {
		t.Error("equilibrium should have reset after changing value")
	}
}

func TestEquilibriumWithNoWrites(t *testing.T) {
	b := NewBoard(LastWriteWins)
	for i := 0; i < 5; i++ {
		b.NextRound()
	}
	if !b.NoWritesForRounds(3) {
		t.Error("5 empty rounds should satisfy 3-round equilibrium")
	}
}

func TestSnapshotSortedByKey(t *testing.T) {
	b := NewBoard(LastWriteWins)
	keys := []string{"z", "a", "m", "b"}
	for i, k := range keys {
		b.Write(k, i, "x")
	}
	snap := b.Snapshot()
	if len(snap) != 4 {
		t.Fatalf("snapshot size: %d", len(snap))
	}
	expected := []string{"a", "b", "m", "z"}
	for i, e := range snap {
		if e.Key != expected[i] {
			t.Errorf("snap[%d].Key: got %s want %s", i, e.Key, expected[i])
		}
	}
}

func TestConcurrentWrites(t *testing.T) {
	// Simple race test — many concurrent writers should not corrupt state.
	b := NewBoard(LastWriteWins)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("k%d-%d", id, j)
				b.Write(key, j, fmt.Sprintf("w%d", id))
			}
		}(i)
	}
	wg.Wait()
	if b.WriteCount() != 1000 {
		t.Errorf("expected 1000 writes, got %d", b.WriteCount())
	}
}
