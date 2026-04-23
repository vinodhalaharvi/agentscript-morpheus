# Multi-Agent Coordination Examples

These 7 `.as` files demonstrate AgentScript's approach to multi-agent coordination.
They define the target syntax — **the runtime that executes these does not exist
yet.** These files are the specification-by-example for the coordination layer.

## The 4×4 matrix

Multi-agent systems can be decomposed along two orthogonal axes:

### Coordination models — how agents interact

- **Hierarchical supervisor** — parent-child lifecycle ownership. Supervisors
  spawn, monitor, restart, escalate. Authority flows down, reports flow up.
- **Actor message passing** — isolated units with mailboxes. State is local;
  communication is typed messages. Protocols define coordination.
- **Blackboard** — shared knowledge substrate. Agents read/write to a common
  board. No direct peer communication; coordination emerges from writes.
- **Peer gossip** — decentralized mesh. Agents exchange state with random
  peers. CRDTs handle merge. No central authority.

### Convergence notions — how we know we're done

- **Goal completion** — a predicate over world state returns true.
- **Consensus** — agents agree on a single value via voting/quorum.
- **State equilibrium** — shared state reaches a fixpoint.
- **Belief alignment** — internal models converge within a distance metric.

## Compatibility matrix

Not every pairing is viable. AgentScript supports the **8 natural pairings**
and fails fast on the rest:

|                     | Goal Completion | Consensus | State Equilibrium | Belief Alignment |
|---------------------|:---------------:|:---------:|:-----------------:|:----------------:|
| Supervisor          | ✓               | —         | —                 | —                |
| Actor               | —               | ✓         | —                 | —                |
| Blackboard          | ✓               | —         | ✓                 | ✓                |
| Peer Gossip         | —               | —         | ✓                 | ✓                |

The rejected cells aren't impossible — they're semantic mismatches. A
peer-gossip mesh has no privileged vantage point to evaluate a goal predicate.
An actor protocol's strength is point-to-point messaging, not convergence
on distances. The matrix forces users toward the natural idiom for their
problem.

## The 7 examples

### Canonical diagonals (one per coordination model)

1. **[refactor-legacy-service.as](refactor-legacy-service.as)** —
   Supervisor + Goal Completion. Parent task decomposes a legacy service into
   microservices, each with an owning agent. Supervisor restarts failing
   agents. Goal: all migration plans validate cross-consistently.

2. **[architectural-decision.as](architectural-decision.as)** —
   Actor + Consensus. Three agents debate a database choice via typed
   messages, call a vote when positions are clear, reach 2/3 quorum.

3. **[crossword-solver.as](crossword-solver.as)** —
   Blackboard + State Equilibrium. Three agents with different expertise
   write guesses to a shared crossword board with confidence scores. Solved
   when no agent proposes a change for N rounds.

4. **[federated-kg-sync.as](federated-kg-sync.as)** —
   Peer Gossip + State Equilibrium. Five knowledge graph instances in
   different regions gossip deltas peer-to-peer. CRDTs merge conflicts.
   Converged when all peers are "quiet" for N rounds.

### Off-diagonals (blackboard and gossip each span 3 convergence modes)

5. **[market-analysis-consensus.as](market-analysis-consensus.as)** —
   Blackboard + Belief Alignment. Three analysts post beliefs with evidence
   to a shared board. Aligned when cross-agent belief vectors are within
   a cosine-similarity threshold.

6. **[documentation-buildout.as](documentation-buildout.as)** —
   Blackboard + Goal Completion. Agents opportunistically contribute to an
   API doc tree. Complete when no placeholders remain and cross-references
   are consistent.

7. **[distributed-threat-model.as](distributed-threat-model.as)** —
   Peer Gossip + Belief Alignment. Security teams at different organizations
   share threat scores via bilateral-trust gossip. Aligned when mesh-wide
   variance drops below threshold.

## What unifies every example

- Outer grammar: `converge "name" ( coordination X convergence Y ... )`
- Pattern matching (`match { ... }`, `receive { ... }`, `on X =>`) for dispatch
- `witness { ... }` block structures the result, same shape across all cells
- Agent declarations as typed handles (`agent "name" claude (system ...)`)
- `context (...)` for shared configuration

**The outer grammar is constant. The inner runtime varies by coordination
model.** That's the architectural claim: one DSL surface, four runtime
strategies, compositional across all existing primitives (Kleisli `>=>`,
FanOut `<*>`, Converge).

## What's rejected (fails fast at parse time)

Attempting an incompatible combination produces a pedagogical error:

```
coordination peer_gossip
convergence  goal_completion

ERROR: incompatible coordination/convergence pair at line 3

  peer_gossip coordination is decentralized — no privileged vantage exists
  to evaluate a goal predicate across the full mesh.

  Consider one of:
    - blackboard + goal_completion
        (shared state makes goals queryable)
    - peer_gossip + state_equilibrium
        (natural for emergent completion on a mesh)
    - peer_gossip + belief_alignment
        (soft convergence across distributed peers)

  See examples/coordination/ for each pattern.
```

## Status

- [x] Grammar designed (this directory)
- [x] Compatibility matrix specified
- [ ] Parser recognition of `coordination`/`convergence` keywords
- [ ] Parser validation against compatibility matrix
- [ ] Supervisor runtime (child lifecycle, restart strategies)
- [ ] Actor runtime (mailboxes, selective receive, protocols)
- [ ] Blackboard runtime (tuple-space, subscriptions, notifications)
- [ ] Gossip runtime (CRDT library, anti-entropy scheduler)
- [ ] Per-cell convergence predicates

The 7 `.as` files serve as the acceptance tests. When the implementation can
parse, validate, and execute all 7 end-to-end, the coordination layer is shipped.
