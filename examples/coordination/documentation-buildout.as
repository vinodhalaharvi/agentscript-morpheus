// =======================================================================
// documentation-buildout.as
// =======================================================================
// Coordination: blackboard
// Convergence:  goal completion
//
// Multiple agents collaborate on API documentation. The board IS the doc
// tree. Agents subscribe to sections matching their capability — an
// endpoint-documenter only reacts to placeholder endpoints; an example-
// generator only reacts to drafts missing examples. The work is
// opportunistic: each agent contributes when it can.
//
// Goal: all sections reviewed, every endpoint has ≥2 examples, cross-
// references resolve.
//
// Why blackboard+goal: data-driven contribution without central scheduling.
// The goal predicate queries the board directly. Agents don't coordinate
// with each other — they coordinate with the state.
// =======================================================================

read "api/openapi.json" >=>
converge "build-docs" (
  coordination blackboard
  convergence  goal_completion

  context (
    session    claude
    max_rounds 25
  )

  blackboard (
    schema {
      sections: tree<{
        path:      string,
        status:    [placeholder | draft | reviewed],
        content:   text,
        examples:  list,
        reviewers: list<agent_id>
      }>
    }
  )

  agent "endpoint-documenter" claude (
    system "Write endpoint docs: parameters, responses, error codes, examples.
            Use the existing style guide."
    subscribe (
      on section_added where type == "endpoint" and status == "placeholder" =>
        draft_documentation
      on section_reviewed where references_my_endpoint =>
        update_cross_references
    )
  )

  agent "example-generator" claude (
    system "Generate realistic request/response examples. Show at least
            one happy path and one error case per endpoint."
    subscribe (
      on section where status == "draft" and missing_examples =>
        add_examples
    )
  )

  agent "consistency-reviewer" claude (
    system "Ensure terminology, casing, and style are consistent across
            sections. Gate review — you mark sections 'reviewed' or
            'draft' based on your verdict."
    subscribe (
      on section where status == "draft" and has_examples =>
        match review_outcome {
          {consistent: true, minor_issues: []}          => mark_reviewed
          {consistent: true, minor_issues: $issues}     => annotate_and_review $issues
          {consistent: false, conflicts: $c}            => propose_reconciliation $c
          {consistent: false, blocking: true, why: $w}  => return_to_draft with_feedback $w
        }
    )
  )

  goal (
    all_sections.status == reviewed
    and examples_per_endpoint >= 2
    and cross_reference_integrity_holds
  )

  witness {
    doc_tree:            $blackboard.sections
    total_endpoints:     $blackboard.sections.filter(s => s.type == "endpoint").count
    contributions:       group_by_agent($blackboard.sections)
    rounds_to_complete:  $round
  }
)
