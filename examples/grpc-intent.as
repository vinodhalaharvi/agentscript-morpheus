// ============================================
// Go gRPC Project — Intent-Driven Generation
// ============================================

// Context — gathered every loop
context (
  sandbox "my-grpc-service/"
  reasoner claude
  history 3

  read "proto/"
  exec "tree"
  exec "buf lint 2>&1"
  exec "buf generate 2>&1"
  exec "go build ./... 2>&1"
  exec "go test ./... 2>&1"
)

// Intent — pure text goals
intent 5 30 propose (
  `Go gRPC service from the proto files in proto/.
   buf.yaml and buf.gen.yaml for code generation.
   buf generate outputs Go + gRPC stubs into gen/.
   internal/server/ implements each service defined in the protos.
   Makefile with targets: generate, build, test, lint, docker.
   Dockerfile multi-stage: buf generate then go build.
   go.mod with google.golang.org/grpc and google.golang.org/protobuf.`
)

// Validate — hard exit criteria
validate (
  exec "buf lint"
  exec "buf generate"
  exec "ls gen/user/v1/user_grpc.pb.go"
  exec "go build ./..."
  exec "go test ./..."
)
