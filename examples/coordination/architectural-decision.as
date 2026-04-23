// =======================================================================
// architectural-decision.as
// =======================================================================
// Coordination: actor message passing
// Convergence:  consensus (2 of 3 quorum)
//
// Three agents evaluate a proposed database choice. They exchange typed
// messages (claims with evidence), challenge each other, and cast votes
// when positions stabilize. A moderator tracks rounds and calls the vote.
// Consensus when any 2 of the 3 agree on a choice.
//
// Why actor+consensus: classic debate/vote protocol. Each agent owns
// local reasoning state. Communication is explicit and typed. The
// consensus primitive is native to this paradigm (Paxos/Raft shape).
// =======================================================================

read "proposal.md" >=>
converge "db-choice" (
  coordination actor_message_passing
  convergence  consensus

  context (
    session    claude
    quorum     2_of_3
    max_rounds 6
  )

  actor "pragmatist" claude (
    system "You favor battle-tested tech. Challenge scaling claims, ask for
            benchmarks, weigh operational cost over elegance."
    receive {
      {from: $other, type: "claim", content: $c, evidence: $e} =>
        match evaluate($c, $e) {
          {stance: "agree"}    => reply {from: me, type: "concur", with: $other}
          {stance: "disagree"} => reply {from: me, type: "counter", claim: rebut($c)}
          {stance: "unsure"}   => reply {from: me, type: "request", need: "benchmark"}
        }
      {type: "call_vote"} =>
        cast_vote based_on my_strongest_position
    }
  )

  actor "idealist" claude (
    system "You favor modern tech with strong primitives. Argue from first
            principles. Accept higher operational cost for better correctness."
    receive {
      {from: $other, type: "claim", content: $c}  => steelman_or_rebut($c)
      {from: $other, type: "counter", claim: $c}  => defend_or_concede($c)
      {type: "call_vote"}                         => cast_vote
    }
  )

  actor "moderator" claude (
    system "Track the debate. Call a vote when positions are clearly formed
            or max rounds approaches."
    receive {
      {type: "round_complete", round: $r} when $r >= 3 =>
        broadcast {type: "call_vote"}
      {type: "round_complete", round: $r} when $r < 3 =>
        increment round
      {type: "vote_cast", from: $a, choice: $c} =>
        tally_and_check_quorum
    }
  )

  consensus (
    value  chosen_database
    from   [pragmatist, idealist, moderator]
    quorum 2_of_3
  )

  witness {
    decision:  $chosen_database
    voters:    [$pragmatist.vote, $idealist.vote, $moderator.vote]
    rounds:    $moderator.round_count
    transcript: all_messages
  }
)
