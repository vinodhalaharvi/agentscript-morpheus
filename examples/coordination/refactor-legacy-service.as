// =======================================================================
// refactor-legacy-service.as
// =======================================================================
// Coordination: hierarchical supervisor
// Convergence:  goal completion
//
// A monolithic legacy service is being split into 3 microservices. A
// supervisor agent spawns one planner per target service. Each planner
// produces a migration plan as structured JSON. The supervisor restarts
// crashed planners (up to 3 times each). The job is done when all three
// plans validate AND they're cross-consistent (auth referenced by billing,
// billing subscribed by notifications).
//
// Why supervisor+goal: decomposable job, named owner per sub-task, natural
// hierarchy of accountability, explicit completion predicate over outputs.
// =======================================================================

read "legacy-service/" >=>
converge "decompose-legacy" (
  coordination supervisor
  convergence  goal_completion

  context (
    sandbox "/tmp/migration"
    session claude
    max_rounds 8
  )

  supervisor "migration-lead" (
    strategy      one_for_one    // restart only the failed child
    max_restarts  3
    within        60s

    child "auth-planner" claude (
      system "Plan extracting authentication into a standalone gRPC service."
      output_schema auth_migration_plan
    )

    child "billing-planner" claude (
      system "Plan extracting billing. Must call the auth service for tokens."
      output_schema billing_migration_plan
      depends_on    auth-planner  // only runs after auth-planner completes
    )

    child "notifications-planner" claude (
      system "Plan extracting notifications. Must subscribe to billing events."
      output_schema notification_migration_plan
      depends_on    billing-planner
    )

    on_child_exit {
      {reason: "normal"}                              => record_completion
      {reason: "crash", child: $c, restarts: $r} when $r < 3
                                                      => restart $c
      {reason: "crash", restarts: $r} when $r >= 3    => escalate
      {reason: "timeout", child: $c}                  => escalate
    }
  )

  goal (
    all_children_completed
    and auth_migration_plan.gateway_defined
    and billing_migration_plan.auth_integrated
    and notification_migration_plan.billing_subscribed
  )

  witness {
    plans: [
      $auth_migration_plan,
      $billing_migration_plan,
      $notification_migration_plan
    ]
    supervisor_report: $migration-lead.report
    rounds_to_completion: $round
  }
)
