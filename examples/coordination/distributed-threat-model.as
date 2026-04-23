// =======================================================================
// distributed-threat-model.as
// =======================================================================
// Coordination: peer gossip (bilateral trust)
// Convergence:  belief alignment
//
// Security teams at different organizations build threat models
// independently. They share findings peer-to-peer, but only with
// explicitly trusted peers (no central coordinator — trust boundaries
// prevent it). Converged when mesh-wide variance on threat scores drops
// below threshold.
//
// Why gossip+alignment: cross-org coordination where hard consensus is
// impractical but soft alignment is valuable. Different orgs may have
// different evidence bases and can't all see everything — alignment is
// the strongest form of "agreement" they can honestly achieve.
// =======================================================================

converge "align-threat-models" (
  coordination peer_gossip
  convergence  belief_alignment

  context (
    max_rounds           15
    gossip_fanout         2
    alignment_threshold   0.90
    trust_policy         bilateral_only
  )

  peers (
    "org-acme"   claude (trusts ["org-beta", "org-gamma"])
    "org-beta"   claude (trusts ["org-acme", "org-gamma"])
    "org-gamma"  claude (trusts ["org-acme", "org-beta", "org-delta"])
    "org-delta"  claude (trusts ["org-gamma"])
  )

  gossip_step (
    on round (
      $me.trusted_peers.sample(gossip_fanout) => $targets
      fanout $targets (
        exchange_threat_scores_with $target =>
          match exchange_result {
            {their_score: $s, evidence: $e} when stronger_evidence($e) =>
              update_my_score weighted_by_evidence $e
            {conflict: $c, fatal: true} =>
              abort_with_conflict $c
            {aligned: true} =>
              $me.mark_aligned_with $target
            _ =>
              retain_my_position
          }
      )
    )
  )

  belief_alignment (
    topics        ["attack_vectors", "impact_severity", "likelihood"]
    metric        weighted_distance
    predicate     mesh_wide_variance < (1.0 - alignment_threshold)
  )

  witness {
    final_threat_model:  weighted_merge(all_peers)
    per_peer_scores:     peers.map(p => {id: p.id, scores: p.scores})
    alignment_graph:     who_aligned_with_whom
    holdouts:            peers.filter(p => not p.aligned_with_majority)
    rounds:              $round
  }
)
