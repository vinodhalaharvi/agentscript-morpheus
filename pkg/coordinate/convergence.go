package coordinate

import (
	"fmt"

	"github.com/vinodhalaharvi/agentscript/pkg/coordinate/blackboard"
	"github.com/vinodhalaharvi/agentscript/pkg/matching"
)

// ConvergencePredicate is the unified interface for deciding when a
// coordination run is complete. Every convergence notion implements this.
//
// The key insight: all four convergence notions (goal, consensus, state
// equilibrium, belief alignment) are predicate + witness. Satisfied()
// returns whether we're done; Witness() returns the result structure
// for the caller.
type ConvergencePredicate interface {
	// Satisfied returns true when the run should terminate successfully.
	// Called after each coordination round.
	Satisfied() bool

	// Witness returns the result payload when Satisfied() is true.
	// Result is a structured value (typically a map) that becomes the
	// output of the coordinate block.
	Witness() matching.Value

	// Description returns a human-readable summary used in log output.
	Description() string
}

// ================================================================
// State equilibrium: no writes for N rounds
// ================================================================

// StateEquilibrium is the predicate for blackboard+state_equilibrium.
// It fires when the board has had no writes for stabilityRounds
// consecutive rounds.
type StateEquilibrium struct {
	board           *blackboard.Board
	stabilityRounds int
	maxRounds       int
}

// NewStateEquilibrium creates the predicate. stabilityRounds is the
// number of consecutive write-free rounds required to declare equilibrium.
func NewStateEquilibrium(board *blackboard.Board, stabilityRounds, maxRounds int) *StateEquilibrium {
	if stabilityRounds <= 0 {
		stabilityRounds = 3
	}
	return &StateEquilibrium{
		board:           board,
		stabilityRounds: stabilityRounds,
		maxRounds:       maxRounds,
	}
}

func (s *StateEquilibrium) Satisfied() bool {
	return s.board.NoWritesForRounds(s.stabilityRounds)
}

func (s *StateEquilibrium) Witness() matching.Value {
	snap := s.board.Snapshot()
	entries := make([]matching.Value, 0, len(snap))
	for _, e := range snap {
		entries = append(entries, map[string]interface{}{
			"key":   e.Key,
			"value": e.Value,
			"by":    e.By,
			"round": float64(e.Round),
		})
	}
	return map[string]interface{}{
		"entries":            entries,
		"entry_count":        float64(len(snap)),
		"write_count":        float64(s.board.WriteCount()),
		"last_write_round":   float64(s.board.LastWriteRound()),
		"stable_since_round": float64(s.board.LastWriteRound() + 1),
		"current_round":      float64(s.board.CurrentRound()),
	}
}

func (s *StateEquilibrium) Description() string {
	return fmt.Sprintf("state_equilibrium (no writes for %d rounds)", s.stabilityRounds)
}

// ================================================================
// Convergence factory — builds the right predicate for a (coord, conv) pair
// ================================================================

// BuildConvergence constructs a ConvergencePredicate for the given
// coordination/convergence pair. Returns an error if the pair is not
// yet implemented (the matrix already validated compatibility).
//
// For blackboard+state_equilibrium (Phase 1), takes the shared board.
// Future cells will take additional args (voter set for consensus, etc.).
func BuildConvergence(coord, conv string, board *blackboard.Board, cfg ConvergenceConfig) (ConvergencePredicate, error) {
	switch {
	case coord == CoordBlackboard && conv == ConvStateEquilibrium:
		return NewStateEquilibrium(board, cfg.StabilityRounds, cfg.MaxRounds), nil
	}
	return nil, fmt.Errorf("coordination/convergence pair %q/%q has no predicate builder yet", coord, conv)
}

// ConvergenceConfig holds tunable parameters for convergence predicates.
// Parser fills this from the context block.
type ConvergenceConfig struct {
	StabilityRounds    int     // for state_equilibrium
	MaxRounds          int     // hard cap across all convergence types
	AlignmentThreshold float64 // for belief_alignment (future)
	Quorum             string  // for consensus, e.g. "2_of_3" (future)
}
