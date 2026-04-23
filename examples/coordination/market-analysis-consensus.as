// =======================================================================
// market-analysis-consensus.as
// =======================================================================
// Coordination: blackboard
// Convergence:  belief alignment
//
// Three financial analysts — bull, bear, macro — build views of a market.
// They post beliefs (with evidence and confidence) to a shared board.
// When a new belief conflicts with an existing one, they react:
// stronger evidence wins; otherwise they post counter-evidence. Aligned
// when cross-agent belief vectors are within a cosine-similarity threshold
// on the key propositions.
//
// Why blackboard+alignment: semantic convergence rather than structural.
// Two agents can express the same view differently and still count as
// aligned. The board holds evidence; the convergence is about meaning.
// =======================================================================

read "market-data/" >=>
converge "align-market-view" (
  coordination blackboard
  convergence  belief_alignment

  context (
    session             claude
    alignment_threshold 0.85
    alignment_metric    cosine_similarity
    max_rounds          12
  )

  blackboard (
    schema {
      beliefs:            map<proposition_id, list<{
                            agent:      id,
                            confidence: float,
                            evidence:   list<citation>,
                            posted_at:  round
                          }>>
      consensus_beliefs:  map<proposition_id, aggregated_belief>
    }
  )

  agent "bull-analyst" claude (
    system "You identify growth signals. Be specific about drivers, magnitudes,
            and timelines. Cite sources."
    subscribe (
      on belief_posted where conflicts_with_mine =>
        match evaluate_conflict {
          {their_evidence: $e} when stronger_than_mine($e) =>
            update_my_belief weighted_by $e
          {their_confidence: $c} when $c < my_confidence =>
            post_counter_evidence
          {fundamental_disagreement: true} =>
            flag_for_reconciliation
        }
    )
  )

  agent "bear-analyst" claude (
    system "You identify risk signals. Quantify downside specifically."
    subscribe (
      on belief_posted where topic in my_focus =>
        evaluate_then_post_or_revise
    )
  )

  agent "macro-analyst" claude (
    system "Connect market signals to broader economic trends. You often
            reconcile bull/bear views by introducing a time horizon axis."
    subscribe (
      on belief_posted =>
        match my_analysis {
          {adds_macro_context: true} => post_contextualized_view
          {reconciles_conflict: true, synthesis: $s} => post_synthesis $s
          _ => no_op
        }
    )
  )

  belief_alignment (
    propositions  ["market_direction", "risk_level", "time_horizon"]
    predicate     avg_pairwise_distance < (1.0 - alignment_threshold)
  )

  witness {
    aligned_beliefs:    $blackboard.consensus_beliefs
    final_distance:     pairwise_distances
    rounds_to_align:    $round
    dissenting_points:  propositions.filter(p => distance(p) > 0.3)
  }
)
