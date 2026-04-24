// ============================================
// Proto injection demo — hardened against silent failures
// ============================================
// Each MCP operation is a separate step so failures surface
// immediately. Validations cannot pass vacuously on an empty repo —
// they require actual .go files to exist and real code to build.
//
// BEFORE RUNNING:
//   - Replace MY-FORK-USER with your GitHub username in this
//     file and in examples/task.proto.
//   - Run from agentscript-morpheus repo root so examples/task.proto
//     resolves.
//   - Ensure CLAUDE_API_KEY and GITHUB_PERSONAL_ACCESS_TOKEN are set.
// ============================================

// --- step 1: fresh workspace ---------------------------------------
exec "rm -rf /tmp/ascript-layout && mkdir -p /tmp/ascript-layout"

  // --- step 2: connect MCP servers upfront ------------------------
  >=> mcp_connect "github" "npx -y @modelcontextprotocol/server-github"
  >=> mcp_connect "git"    "npx -y @cyanheads/git-mcp-server"

  // --- step 3: fork the upstream repo ---------------------------
  >=> mcp_agent "github" "use the fork_repository tool to fork owner 'golang-standards' repo 'project-layout' to my account"

  // --- step 4: let GitHub replicate -------------------------------
  >=> exec "sleep 5"

  // --- step 5: clone — single atomic operation -------------------
  >=> mcp_agent "git" "use the git_clone tool with url 'https://github.com/MY-FORK-USER/project-layout' and localPath '/tmp/ascript-layout/repo'"

  // --- step 6: create + checkout feature branch ------------------
  >=> mcp_agent "git" "use the git_branch tool on path '/tmp/ascript-layout/repo' to create a new branch called 'add-grpc-service' and check it out"

  // --- step 7: HARD verify we are on the right branch ------------
  //    `git branch --show-current` prints just the branch name.
  //    grep -Fxq matches the whole line literally. No shell quoting
  //    gymnastics — just check stdout against a literal string.
  >=> exec "cd /tmp/ascript-layout/repo && git branch --show-current | grep -Fxq add-grpc-service"

  // --- step 8: drop the proto in place ---------------------------
  >=> exec "mkdir -p /tmp/ascript-layout/repo/api/proto/service/v1 && cp examples/task.proto /tmp/ascript-layout/repo/api/proto/service/v1/task.proto && test -f /tmp/ascript-layout/repo/api/proto/service/v1/task.proto"

  // --- step 9: AI does the engineering --------------------------
  >=> converge "stand-up-grpc-service" (
        context (
          sandbox "/tmp/ascript-layout/repo"
          session claude
          history 3

          readonly "gen/"

          read  "api/proto/service/v1/task.proto"
          exec  "ls -la"
          exec  "find . -maxdepth 2 -type d -not -path './.git*'"
        )

        intent 12 30 propose (
          `You have a fresh checkout of the golang-standards/project-layout
           template. It has no Go code, just the standard directory skeleton
           with README.md stubs inside cmd/, internal/, etc.

           A proto file sits at api/proto/service/v1/task.proto — read it
           to understand the TaskService interface.

           Goal: stand up a working in-memory gRPC TaskService that builds,
           vets, and passes tests.

           Typical approach, you may deviate if justified:
             - Initialize go.mod for module github.com/MY-FORK-USER/project-layout
             - Create buf.yaml and buf.gen.yaml to drive code generation
             - Run 'buf generate' via a command in your proposal so that
               gen/service/v1/ protobuf Go files actually land on disk.
             - Implement TaskService in internal/service/task_service.go
               using an in-memory map with sync.RWMutex, returning codes.NotFound
               for missing IDs, generating a UUID for CreateTask
             - Entry point in cmd/grpc-server/main.go listening on :50051
             - A bufconn test in internal/service/task_service_test.go

           Constraints:
             - Protobuf bindings in gen/ MUST come from a real code generator
               such as buf generate or protoc. Do NOT hand-write them — the
               engine will reject writes under gen/.
             - Validations require ACTUAL Go files. An empty module passing
               'go build' trivially will not count — the validate step
               checks file counts and specific file presence.
             - Follow project-layout conventions: code in internal/, entry
               point in cmd/, nothing in pkg/.`
        )

        validate (
          // These cannot pass on an empty repo — they require actual code.

          // Must have a real go.mod
          exec "test -f /tmp/ascript-layout/repo/go.mod"
          exec "grep -q '^module github.com/' /tmp/ascript-layout/repo/go.mod"

          // Must have buf configs, or equivalent codegen mechanism that
          // produced the files below
          exec "test -f /tmp/ascript-layout/repo/buf.yaml || test -f /tmp/ascript-layout/repo/buf.gen.yaml || find /tmp/ascript-layout/repo -name 'protoc*.sh' | head -1 | grep -q ."

          // Must have generated protobuf bindings
          exec "find /tmp/ascript-layout/repo/gen -name '*.pb.go' 2>/dev/null | head -1 | grep -q ."

          // Must have the service implementation, entrypoint, and test
          exec "test -f /tmp/ascript-layout/repo/internal/service/task_service.go"
          exec "test -f /tmp/ascript-layout/repo/cmd/grpc-server/main.go"
          exec "find /tmp/ascript-layout/repo -name '*_test.go' -not -path '*/gen/*' -not -path '*/.git/*' | head -1 | grep -q ."

          // Must actually build, vet, and test — with real code present
          exec "cd /tmp/ascript-layout/repo && go build ./..."
          exec "cd /tmp/ascript-layout/repo && go vet ./..."
          exec "cd /tmp/ascript-layout/repo && go test ./..."
        )
      )

  // --- step 10: final safety check before pushing ---------------
  //    Even if converge reports success, verify the expected artifacts
  //    before we push anything. Fail loud if something is off.
  >=> exec "cd /tmp/ascript-layout/repo && ls internal/service/task_service.go cmd/grpc-server/main.go > /dev/null && find gen -name '*.pb.go' | head -1 | grep -q . && echo 'Artifacts verified.'"

  // --- step 11: commit + push ------------------------------------
  >=> mcp_agent "git" "use git_add on path '/tmp/ascript-layout/repo' to stage everything. Then use git_commit on the same path with message 'feat: add gRPC TaskService via proto injection'. Then use git_push on the same path to push the current branch to origin."

  // --- step 12: open a PR ----------------------------------------
  >=> mcp_agent "github" "use the create_pull_request tool. owner is 'MY-FORK-USER'. repo is 'project-layout'. title is 'feat: add gRPC TaskService'. head is 'add-grpc-service'. base is 'master'. body is 'Generated end-to-end by AgentScript: fork, clone, proto injection, AI-driven scaffolding, real buf generate, validation, commit, push. All validations pass against real code.'"
