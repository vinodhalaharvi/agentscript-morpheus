// ============================================
// Patch Mode — Add feature to existing repo
// ============================================
// Works on an existing git repo.
// Creates a branch, proposes changes as patches,
// auto-commits each accepted proposal.
// ============================================

converge "add-rate-limiting" (
  sandbox "."
  reasoner claude
  mode patch
  branch "feature/add-rate-limiting"
  base "main"
  auto_commit true
  auto_rebase true
  commit_prefix "converge"
  history 3

  context (
    exec "git log --oneline -10"
    exec "git status"
    exec "git diff --stat main 2>&1"
    exec "tree internal/ cmd/ 2>&1"
    exec "go build ./... 2>&1"
    exec "go test ./... 2>&1"
  )

  intent 5 30 propose (
    `Add rate limiting middleware to the gRPC server.

     DIRECTORY STRUCTURE:
     internal/
     └── middleware/
         ├── ratelimit.go          — token bucket per client IP using golang.org/x/time/rate
         └── ratelimit_test.go     — table-driven tests: under limit, at limit, over limit

     cmd/server/main.go            — wire ratelimit into the interceptor chain

     BEHAVIOR:
     Use golang.org/x/time/rate with 100 req/s per IP burst 20.
     Extract client IP from peer.FromContext.
     Return codes.ResourceExhausted with "rate limit exceeded" message.
     Sync.Map to store per-IP limiters.
     Cleanup stale limiters after 5 minutes.
     Only modify: middleware/ratelimit.go (new), ratelimit_test.go (new), main.go (wire).`
  )

  validate (
    exec "go build ./..."
    exec "go test ./..."
    exec "go vet ./..."
    exec "test -f internal/middleware/ratelimit.go"
    exec "test -f internal/middleware/ratelimit_test.go"
  )
)
