// =======================================================================
// federated-kg-sync.as
// =======================================================================
// Coordination: peer gossip
// Convergence:  state equilibrium (CRDT-stable)
//
// Five knowledge graph instances in different regions. Each holds
// different facts due to local ingestion. They gossip deltas peer-to-peer
// — each round, each peer contacts 2 random others and exchanges state.
// CRDTs handle conflict resolution. Converged when all peers report
// "quiet" (no new deltas) for 3 rounds.
//
// Why gossip+equilibrium: classic CRDT use case. No central coordinator
// (cross-region trust/latency concerns). Equilibrium is the natural
// completion signal — the protocol IS the convergence mechanism.
// =======================================================================

converge "federate-knowledge" (
  coordination peer_gossip
  convergence  state_equilibrium

  context (
    max_rounds          20
    gossip_fanout        2     // each round, contact 2 random peers
    anti_entropy_period  5s
  )

  peers (
    "kg-us-east"  claude (source "postgres://us-east/kg")
    "kg-us-west"  claude (source "postgres://us-west/kg")
    "kg-eu"       claude (source "postgres://eu/kg")
    "kg-ap"       claude (source "postgres://ap/kg")
    "kg-sa"       claude (source "postgres://sa/kg")
  )

  crdt_type or_set_with_causal_clocks

  gossip_step (
    on round (
      $me.pick_random_peers(gossip_fanout) => $targets
      fanout $targets (
        exchange_delta_with $target =>
          merge_into $me using crdt_type
      )
    )

    on merge_complete {
      {conflicts: [], added: $n} when $n > 0 =>
        $me.advance_clock
      {conflicts: [...$cs]} =>
        $me.crdt_resolve $cs
      {conflicts: [], added: 0} =>
        $me.mark_quiet
    }
  )

  equilibrium (
    predicate all_peers_quiet_for_rounds(3)
  )

  witness {
    merged_graph:           crdt_merge(all_peers)
    rounds_to_convergence:  $round
    per_peer_state:         peers.map(p => {
                              id:    p.id,
                              clock: p.clock,
                              facts: p.facts.size
                            })
  }
)
