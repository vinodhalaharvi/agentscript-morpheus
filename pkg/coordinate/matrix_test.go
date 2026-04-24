package coordinate

import (
	"strings"
	"testing"
)

func TestValidateCompatibility_ValidAndImplemented(t *testing.T) {
	// The one pair that should pass in Phase 1
	err := ValidateCompatibility(CoordBlackboard, ConvStateEquilibrium)
	if err != nil {
		t.Errorf("blackboard + state_equilibrium should be implemented: %v", err)
	}
}

func TestValidateCompatibility_ValidButNotYetImplemented(t *testing.T) {
	tests := []struct {
		coord string
		conv  string
	}{
		// Valid cells per matrix but not implemented in Phase 1
		{CoordSupervisor, ConvGoalCompletion},
		{CoordActor, ConvConsensus},
		{CoordBlackboard, ConvGoalCompletion},
		{CoordBlackboard, ConvBeliefAlignment},
		{CoordPeerGossip, ConvStateEquilibrium},
		{CoordPeerGossip, ConvBeliefAlignment},
	}
	for _, tc := range tests {
		t.Run(tc.coord+"_"+tc.conv, func(t *testing.T) {
			err := ValidateCompatibility(tc.coord, tc.conv)
			if err == nil {
				t.Errorf("expected 'not yet implemented' error, got nil")
				return
			}
			if !strings.Contains(err.Error(), "not yet implemented") {
				t.Errorf("expected 'not yet implemented' message, got: %v", err)
			}
		})
	}
}

func TestValidateCompatibility_IncompatiblePairs(t *testing.T) {
	incompatible := []struct {
		coord string
		conv  string
	}{
		// Peer gossip rejects goal completion (no privileged vantage)
		{CoordPeerGossip, ConvGoalCompletion},
		// Peer gossip rejects strong consensus
		{CoordPeerGossip, ConvConsensus},
		// Supervisor rejects consensus (authority vs negotiation)
		{CoordSupervisor, ConvConsensus},
		// Actor rejects goal completion (no shared state)
		{CoordActor, ConvGoalCompletion},
		// Blackboard rejects consensus (no voting primitive)
		{CoordBlackboard, ConvConsensus},
	}
	for _, tc := range incompatible {
		t.Run(tc.coord+"_"+tc.conv, func(t *testing.T) {
			err := ValidateCompatibility(tc.coord, tc.conv)
			if err == nil {
				t.Errorf("expected incompatibility error, got nil")
				return
			}
			if !strings.Contains(err.Error(), "incompatible") {
				t.Errorf("expected 'incompatible' message, got: %v", err)
			}
			// Should offer alternatives
			if !strings.Contains(err.Error(), "use one of") {
				t.Errorf("expected pedagogical alternatives in error: %v", err)
			}
		})
	}
}

func TestValidateCompatibility_UnknownIDs(t *testing.T) {
	err := ValidateCompatibility("not_a_real_thing", ConvStateEquilibrium)
	if err == nil || !strings.Contains(err.Error(), "unknown coordination") {
		t.Errorf("expected 'unknown coordination' error, got: %v", err)
	}
	err = ValidateCompatibility(CoordBlackboard, "not_a_real_thing")
	if err == nil || !strings.Contains(err.Error(), "unknown convergence") {
		t.Errorf("expected 'unknown convergence' error, got: %v", err)
	}
}
