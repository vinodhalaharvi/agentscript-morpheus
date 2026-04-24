package main

import (
	"testing"
)

func TestHasIntentBlocks_TopLevelIntent(t *testing.T) {
	intentFile := `context (
  sandbox "/tmp"
)

intent 5 60 propose (
  "do stuff"
)

validate (
  exec "test"
)`
	if !hasIntentBlocks(intentFile) {
		t.Error("top-level intent file should be detected")
	}
}

// TestHasIntentBlocks_CoordinateWithNestedContext is the regression test
// for the rung1-greeter bug: a coordinate block with a nested context(...)
// inside was being misclassified as intent-style, causing intent.ParseFile
// to eat the context block, which left max_rounds/stability_rounds at
// default values downstream.
func TestHasIntentBlocks_CoordinateWithNestedContext(t *testing.T) {
	coordFile := `coordinate "rung1-greeter" (
  coordination blackboard
  convergence  state_equilibrium

  context (
    session          claude
    stability_rounds 2
    max_rounds       6
  )

  agent "greeter" claude (
    system "a"
    subscribe (
      on __tick__/* matching ` + "`_`" + ` => "act"
    )
  )
)`
	if hasIntentBlocks(coordFile) {
		t.Error("coordinate file with nested context should NOT be classified as intent")
	}
}

func TestHasIntentBlocks_ConvergeWithNestedContext(t *testing.T) {
	convergeFile := `converge "demo" (
  context (
    sandbox "/tmp/x"
    session claude
  )
  intent 3 30 propose (
    "do it"
  )
  validate (
    exec "go test ./..."
  )
)`
	if hasIntentBlocks(convergeFile) {
		t.Error("converge-wrapped intent blocks should NOT match top-level detection")
	}
}

func TestHasIntentBlocks_PurePipeline(t *testing.T) {
	if hasIntentBlocks("") {
		t.Error("empty content should not match")
	}
	if hasIntentBlocks(`exec "echo hi"`) {
		t.Error("plain pipeline should not match")
	}
}

func TestHasIntentBlocks_LegacyTopLevelContext(t *testing.T) {
	// Legacy files can have bare `context (...)` at top level — still supported.
	legacy := `context (
  sandbox "/tmp"
)

exec "do something"`
	if !hasIntentBlocks(legacy) {
		t.Error("legacy top-level context should still match")
	}
}
