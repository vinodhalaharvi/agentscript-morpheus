// ============================================
// Multi-Service Generation — Parallel Converge
// ============================================
// Generates two microservices simultaneously,
// each in its own sandbox with independent
// validation loops.
// ============================================

(
  converge "user-service" (
    context (
      sandbox "user-service/"
      reasoner claude
      history 3
      exec "tree"
      exec "go build ./... 2>&1"
      exec "go test ./... 2>&1"
    )

    intent 5 30 propose (
      `Go REST API for user management.
       chi router, sqlx with PostgreSQL.
       CRUD endpoints: GET/POST/PUT/DELETE /users.
       JWT auth middleware.
       cmd/server/main.go entry point.
       internal/handler/, internal/model/, internal/store/.
       Makefile, Dockerfile, go.mod.`
    )

    validate (
      exec "go build ./..."
      exec "go test ./..."
      exec "go vet ./..."
      exec "test -f Dockerfile"
    )
  )

  <*> converge "order-service" (
    context (
      sandbox "order-service/"
      reasoner claude
      history 3
      exec "tree"
      exec "go build ./... 2>&1"
      exec "go test ./... 2>&1"
    )

    intent 5 30 propose (
      `Go microservice for order processing.
       Kafka consumer using segmentio/kafka-go.
       Topics: orders.created, orders.updated.
       PostgreSQL persistence with sqlx.
       cmd/worker/main.go entry point.
       internal/consumer/, internal/handler/, internal/store/.
       Graceful shutdown on SIGTERM.
       Makefile, Dockerfile, go.mod.`
    )

    validate (
      exec "go build ./..."
      exec "go test ./..."
      exec "go vet ./..."
      exec "test -f Dockerfile"
    )
  )
)
>=> merge
>=> save "generation-log.txt"
>=> notify "slack"
