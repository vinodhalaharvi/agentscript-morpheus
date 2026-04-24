package coordinate

import (
	"strings"
	"testing"
)

func TestParseCoordinateBody_Basic(t *testing.T) {
	body := `
coordination blackboard
convergence  state_equilibrium

context (
  session claude
  stability_rounds 3
  max_rounds 30
)

blackboard (
  write_policy higher_confidence_wins
)

agent "vocab-expert" claude (
  system "You know unusual words."
  subscribe (
    on cells/* matching ` + "`" + `{letter: $l, confidence: $c}` + "`" + ` =>
      "react to a new cell update"
  )
)
`
	cfg, err := ParseCoordinateBody("test", body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Coordination != "blackboard" {
		t.Errorf("coordination: got %q", cfg.Coordination)
	}
	if cfg.Convergence != "state_equilibrium" {
		t.Errorf("convergence: got %q", cfg.Convergence)
	}
	if cfg.StabilityRounds != 3 {
		t.Errorf("stability_rounds: got %d", cfg.StabilityRounds)
	}
	if cfg.MaxRounds != 30 {
		t.Errorf("max_rounds: got %d", cfg.MaxRounds)
	}
	if cfg.WritePolicy != "higher_confidence_wins" {
		t.Errorf("write_policy: got %q", cfg.WritePolicy)
	}
	if len(cfg.Agents) != 1 {
		t.Fatalf("agents: got %d", len(cfg.Agents))
	}
	a := cfg.Agents[0]
	if a.ID != "vocab-expert" {
		t.Errorf("agent id: got %q", a.ID)
	}
	if a.System != "You know unusual words." {
		t.Errorf("system prompt: got %q", a.System)
	}
	if len(a.Subscriptions) != 1 {
		t.Fatalf("subscriptions: got %d", len(a.Subscriptions))
	}
	sub := a.Subscriptions[0]
	if sub.KeyFilterGlob != "cells/*" {
		t.Errorf("key filter: got %q", sub.KeyFilterGlob)
	}
	if !strings.Contains(sub.PatternSrc, "letter") {
		t.Errorf("pattern should contain 'letter': got %q", sub.PatternSrc)
	}
	if !strings.Contains(sub.Instruction, "new cell") {
		t.Errorf("instruction: got %q", sub.Instruction)
	}
}

func TestParseCoordinateBody_IncompatiblePair(t *testing.T) {
	body := `
coordination peer_gossip
convergence  goal_completion
agent "x" claude (
  system "foo"
  subscribe (
    on x matching ` + "`_`" + ` => "do it"
  )
)
`
	_, err := ParseCoordinateBody("test", body)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Errorf("expected 'incompatible' message, got: %v", err)
	}
}

func TestParseCoordinateBody_MissingCoordination(t *testing.T) {
	body := `
convergence state_equilibrium
agent "x" claude (
  system "s"
  subscribe ( on x matching ` + "`_`" + ` => "do" )
)
`
	_, err := ParseCoordinateBody("test", body)
	if err == nil {
		t.Fatal("expected missing-coordination error")
	}
	if !strings.Contains(err.Error(), "coordination") {
		t.Errorf("error should mention missing coordination: %v", err)
	}
}

func TestParseCoordinateBody_NoAgents(t *testing.T) {
	body := `
coordination blackboard
convergence  state_equilibrium
`
	_, err := ParseCoordinateBody("test", body)
	if err == nil {
		t.Fatal("expected 'requires at least one agent' error")
	}
}

func TestParseCoordinateBody_MultipleAgents(t *testing.T) {
	body := `
coordination blackboard
convergence  state_equilibrium

agent "a" claude (
  system "first"
  subscribe ( on x matching ` + "`_`" + ` => "do a" )
)

agent "b" claude (
  system "second"
  subscribe ( on y matching ` + "`_`" + ` => "do b" )
)
`
	cfg, err := ParseCoordinateBody("test", body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(cfg.Agents))
	}
	if cfg.Agents[0].ID != "a" || cfg.Agents[1].ID != "b" {
		t.Errorf("agent order wrong: %v, %v", cfg.Agents[0].ID, cfg.Agents[1].ID)
	}
}

func TestMakeKeyFilter(t *testing.T) {
	tests := []struct {
		glob string
		key  string
		want bool
	}{
		{"", "anything", true}, // nil filter treats everything as match
		{"cells/*", "cells/a", true},
		{"cells/*", "cells/", true},
		{"cells/*", "other/a", false},
		{"cells/*", "cells", false}, // no slash, not a match
		{"exact", "exact", true},
		{"exact", "ex", false},
	}
	for _, tc := range tests {
		t.Run(tc.glob+"__"+tc.key, func(t *testing.T) {
			f := makeKeyFilter(tc.glob)
			got := true
			if f != nil {
				got = f(tc.key)
			}
			if got != tc.want {
				t.Errorf("glob %q, key %q: got %v want %v", tc.glob, tc.key, got, tc.want)
			}
		})
	}
}
