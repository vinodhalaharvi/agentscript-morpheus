// Package coordinate implements AgentScript's multi-agent coordination layer.
// It sits as a peer to pkg/intent (converge). Both share pkg/matching for
// pattern-based dispatch.
//
// This file defines the 4x4 compatibility matrix for coordination×convergence
// pairings. Only 8 of 16 combinations ship — the rest produce pedagogical
// errors at parse time. The goal is to prevent users from expressing
// semantic nonsense (e.g., peer gossip + goal completion) and guide them
// to the natural idiom for their problem.
package coordinate

import (
	"fmt"
	"strings"
)

// Coordination model IDs used in DSL: `coordination <id>`
const (
	CoordSupervisor = "supervisor"
	CoordActor      = "actor_message_passing"
	CoordBlackboard = "blackboard"
	CoordPeerGossip = "peer_gossip"
)

// Convergence notion IDs used in DSL: `convergence <id>`
const (
	ConvGoalCompletion   = "goal_completion"
	ConvConsensus        = "consensus"
	ConvStateEquilibrium = "state_equilibrium"
	ConvBeliefAlignment  = "belief_alignment"
)

// ValidCoordinations is the set of recognized coordination model IDs.
var ValidCoordinations = []string{
	CoordSupervisor,
	CoordActor,
	CoordBlackboard,
	CoordPeerGossip,
}

// ValidConvergences is the set of recognized convergence notion IDs.
var ValidConvergences = []string{
	ConvGoalCompletion,
	ConvConsensus,
	ConvStateEquilibrium,
	ConvBeliefAlignment,
}

// compatibilityMatrix encodes the 8 natural pairings.
// Key is coordination; value is the set of compatible convergence notions.
// Any (coord, conv) pair NOT in this table is rejected at parse time.
var compatibilityMatrix = map[string]map[string]bool{
	CoordSupervisor: {
		ConvGoalCompletion: true,
	},
	CoordActor: {
		ConvConsensus: true,
	},
	CoordBlackboard: {
		ConvGoalCompletion:   true,
		ConvStateEquilibrium: true,
		ConvBeliefAlignment:  true,
	},
	CoordPeerGossip: {
		ConvStateEquilibrium: true,
		ConvBeliefAlignment:  true,
	},
}

// implementedMatrix is compatibilityMatrix narrowed to cells that actually
// have a working runtime implementation today. Phase 1 ships only
// blackboard + state_equilibrium. Other cells in compatibilityMatrix are
// valid combinations but produce "not yet implemented" errors.
var implementedMatrix = map[string]map[string]bool{
	CoordBlackboard: {
		ConvStateEquilibrium: true,
		// ConvGoalCompletion:  true, // Phase 2
		// ConvBeliefAlignment: true, // Phase 2
	},
	// CoordSupervisor: {ConvGoalCompletion: true}, // Phase 3 (via Ergo)
	// CoordActor:      {ConvConsensus: true},      // Phase 3 (via Ergo)
	// CoordPeerGossip: {...},                      // Phase 4 (via libp2p)
}

// ValidateCompatibility returns nil if (coord, conv) is a supported pair,
// or a pedagogical error otherwise. The error distinguishes between
// "semantically incompatible" (will never be implemented) and "valid but
// not yet implemented in this version".
func ValidateCompatibility(coord, conv string) error {
	// First: is the coordination ID itself recognized?
	if !knownCoordination(coord) {
		return fmt.Errorf("unknown coordination %q. Valid: %s",
			coord, strings.Join(ValidCoordinations, ", "))
	}
	// Is the convergence ID recognized?
	if !knownConvergence(conv) {
		return fmt.Errorf("unknown convergence %q. Valid: %s",
			conv, strings.Join(ValidConvergences, ", "))
	}

	// Is the pair semantically compatible at all?
	conformants, ok := compatibilityMatrix[coord]
	if !ok || !conformants[conv] {
		return incompatibilityError(coord, conv)
	}

	// Is the pair implemented in this build?
	impl, ok := implementedMatrix[coord]
	if !ok || !impl[conv] {
		return notYetImplementedError(coord, conv)
	}
	return nil
}

func knownCoordination(c string) bool {
	for _, v := range ValidCoordinations {
		if v == c {
			return true
		}
	}
	return false
}

func knownConvergence(c string) bool {
	for _, v := range ValidConvergences {
		if v == c {
			return true
		}
	}
	return false
}

// incompatibilityError produces a pedagogical message explaining WHY the
// pair doesn't make sense and suggesting natural alternatives.
func incompatibilityError(coord, conv string) error {
	// Suggestions: for this convergence notion, which coordination models DO fit?
	var altCoords []string
	for c, convs := range compatibilityMatrix {
		if convs[conv] {
			altCoords = append(altCoords, c)
		}
	}
	// For this coordination model, which convergence notions DO fit?
	var altConvs []string
	for c := range compatibilityMatrix[coord] {
		altConvs = append(altConvs, c)
	}

	explanation := incompatibilityExplanation(coord, conv)

	var sb strings.Builder
	fmt.Fprintf(&sb, "incompatible coordination/convergence pair\n\n")
	fmt.Fprintf(&sb, "  coordination: %s\n", coord)
	fmt.Fprintf(&sb, "  convergence:  %s\n\n", conv)
	fmt.Fprintf(&sb, "  %s\n\n", explanation)
	if len(altCoords) > 0 {
		fmt.Fprintf(&sb, "  For %s convergence, use one of:\n", conv)
		for _, c := range altCoords {
			fmt.Fprintf(&sb, "    - %s\n", c)
		}
		fmt.Fprintln(&sb)
	}
	if len(altConvs) > 0 {
		fmt.Fprintf(&sb, "  For %s coordination, use one of:\n", coord)
		for _, c := range altConvs {
			fmt.Fprintf(&sb, "    - %s\n", c)
		}
		fmt.Fprintln(&sb)
	}
	fmt.Fprintf(&sb, "  See examples/coordination/ for each natural pattern.")
	return fmt.Errorf("%s", sb.String())
}

// incompatibilityExplanation returns the "why" for each rejected cell.
// These are the pedagogical strings users see most often.
func incompatibilityExplanation(coord, conv string) string {
	switch coord {
	case CoordSupervisor:
		switch conv {
		case ConvConsensus:
			return "Supervisor coordination is authority-driven (parent dictates child lifecycle). Consensus needs peer negotiation among equals. Express this as actors under a supervisor that reach consensus among themselves."
		case ConvStateEquilibrium:
			return "Supervisor coordination is command-driven, not state-reactive. State equilibrium is an emergent property of shared state, not supervision."
		case ConvBeliefAlignment:
			return "Supervisor coordination doesn't expose a substrate for cross-agent belief comparison. Belief alignment needs shared state (blackboard) or peer exchange (gossip)."
		}
	case CoordActor:
		switch conv {
		case ConvGoalCompletion:
			return "Actor coordination is point-to-point messaging, not state-centric. Goal predicates need a privileged vantage point over world state — use supervisor + goal or blackboard + goal instead."
		case ConvStateEquilibrium:
			return "Actors maintain local state; there's no natural shared state for equilibrium. If you need emergent stability, use blackboard + state_equilibrium."
		case ConvBeliefAlignment:
			return "Actor messaging is peer-to-peer by address; belief alignment needs either a shared substrate (blackboard) or mesh exchange (gossip)."
		}
	case CoordBlackboard:
		switch conv {
		case ConvConsensus:
			return "Blackboards don't have a natural voting primitive — agents write independently. Use actor + consensus for vote-based agreement."
		}
	case CoordPeerGossip:
		switch conv {
		case ConvGoalCompletion:
			return "Peer gossip is decentralized — no single peer has a privileged vantage to evaluate a goal predicate over the full mesh. Use blackboard + goal (centralized state) or peer_gossip + state_equilibrium (emergent completion)."
		case ConvConsensus:
			return "Gossip reaches eventual consistency, not strong consensus. For voting/quorum semantics use actor + consensus. For eventual agreement use peer_gossip + state_equilibrium."
		}
	}
	return "This combination is not a natural fit."
}

// notYetImplementedError explains that the pair is valid but unimplemented.
func notYetImplementedError(coord, conv string) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "coordination %q with convergence %q is a valid combination but not yet implemented in this version.\n\n", coord, conv)
	fmt.Fprintf(&sb, "  Currently implemented cells:\n")
	for c, convs := range implementedMatrix {
		for v := range convs {
			fmt.Fprintf(&sb, "    - %s + %s\n", c, v)
		}
	}
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "  See examples/coordination/README.md for the full roadmap.")
	return fmt.Errorf("%s", sb.String())
}
