// =======================================================================
// crossword-solver.as
// =======================================================================
// Coordination: blackboard
// Convergence:  state equilibrium
//
// Three agents collaboratively solve a crossword. Each has distinct
// expertise (unusual words, crossword conventions, constraint propagation).
// They write guesses to a shared board with confidence scores. Higher
// confidence overwrites lower. Solved when no writes occur for 3 rounds.
//
// Why blackboard+equilibrium: agents don't need to know about each other.
// They react to board state. Convergence is emergent — we're done when
// nobody has anything more to contribute.
// =======================================================================

read "puzzle.json" >=>
converge "solve-crossword" (
  coordination blackboard
  convergence  state_equilibrium

  context (
    session          claude
    stability_rounds 3
    max_rounds       30
  )

  blackboard (
    schema {
      cells:         map<position, {letter: char, confidence: float, by: agent_id}>
      clues_solved:  list<clue_id>
    }
    write_policy higher_confidence_wins
  )

  agent "vocabulary-expert" claude (
    system "You know unusual and rare words. Focus on difficult letter
            combinations and obscure clues."
    subscribe (
      on clue_updated where intersects_my_solutions =>
        match re_evaluate {
          {new_answer: $a, confidence: $c} when $c > current_confidence =>
            write_cells_for $a at confidence $c
          _ =>
            no_op
        }
      on cell_changed where confidence > 0.9 =>
        propagate_constraints_to_my_clues
    )
  )

  agent "crossword-veteran" claude (
    system "You know crossword conventions — themes, common fills, setter
            style. You often see the big picture first."
    subscribe (
      on any_change =>
        match check_theme_consistency {
          {theme_violated: true, cells: [...$cs]} =>
            lower_confidence_on $cs
          {theme_confirmed: true, implications: [...$imp]} =>
            boost_confidence_on $imp
          _ =>
            no_op
        }
    )
  )

  agent "constraint-solver" claude (
    system "You handle letter-intersection logic. Pure structural reasoning
            — ignore semantics, just propagate constraints."
    subscribe (
      on cell_changed =>
        match propagate {
          {contradictions: []}       => solidify_neighbors
          {contradictions: [...$cs]} => lower_confidence_on $cs
        }
    )
  )

  equilibrium (
    predicate no_writes_for_rounds(3)
    or       all_cells_filled_with_confidence(0.8)
  )

  witness {
    final_board:       $blackboard.cells
    stable_since_round: $blackboard.last_write_round
    unsolved:          $blackboard.cells.filter(c => c.confidence < 0.5)
    contributions:     group_by_agent($blackboard.cells)
  }
)
