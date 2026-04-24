package agentscript

import (
	"strings"
	"testing"
)

// TestTripleStringLexer verifies the lexer tokenizes """...""" correctly,
// preserving nested quotes, backticks, and newlines.
func TestTripleStringLexer(t *testing.T) {
	input := `coordinate "x" """body with "nested" quotes
on multiple lines and ` + "`backticks`" + `"""`

	prog, err := Parse(input)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Statements))
	}
	cmd := prog.Statements[0].Command
	if cmd == nil {
		t.Fatal("expected Command not nil")
	}
	if cmd.Action != "coordinate" {
		t.Errorf("action: got %q want coordinate", cmd.Action)
	}
	if cmd.Arg != "x" {
		t.Errorf("arg: got %q want x", cmd.Arg)
	}
	expected := "body with \"nested\" quotes\non multiple lines and `backticks`"
	if cmd.Arg2 != expected {
		t.Errorf("Arg2 mismatch.\nGot:  %q\nWant: %q", cmd.Arg2, expected)
	}
}

// TestTripleQuoteSmokeParse is the exact smoke-test input the user kept
// hitting — a coordinate block with a `convergence` directive inside it.
// This test reproduces the word-boundary bug (converge substring matching
// convergence) and ensures the fix holds.
func TestTripleQuoteSmokeParse(t *testing.T) {
	input := "coordinate \"smoke\" (\n" +
		"  coordination blackboard\n" +
		"  convergence  state_equilibrium\n" +
		"\n" +
		"  context (\n" +
		"    session claude\n" +
		"    stability_rounds 2\n" +
		"    max_rounds 3\n" +
		"  )\n" +
		"\n" +
		"  agent \"participant\" claude (\n" +
		"    system \"You participate. Respond with empty writes.\"\n" +
		"    subscribe (\n" +
		"      on __tick__/* matching `_` => \"Acknowledge tick.\"\n" +
		"    )\n" +
		"  )\n" +
		")"

	// Step 1: preprocess. The bug that was hitting: preprocessConverge
	// saw the word "converge" as a substring of "convergence" and tried
	// to extract a body from the wrong line, corrupting everything.
	s1 := preprocessConverge(input)
	s2 := preprocessBlockCommand(s1, "coordinate")

	// Must be one coordinate statement, not two with a stray converge
	if strings.Contains(s2, `converge "" "`) {
		t.Fatalf("word-boundary bug: 'convergence' was mistaken for 'converge'\noutput:\n%s", s2)
	}
	if !strings.Contains(s2, `coordinate "smoke" """`) {
		t.Errorf("expected triple-quoted coordinate wrapping, got:\n%s", s2)
	}

	// No escape hell artifacts
	if strings.Contains(s2, "|||") {
		t.Errorf("||| encoding should be gone, got:\n%s", s2)
	}
	if strings.Contains(s2, `\"participant\"`) {
		t.Errorf("quotes shouldn't be backslash-escaped, got:\n%s", s2)
	}

	// Step 2: parse through the real participle grammar
	prog, err := Parse(s2)
	if err != nil {
		t.Fatalf("parse failed: %v\n\ninput to parser:\n%s", err, s2)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 statement after parse, got %d", len(prog.Statements))
	}
	cmd := prog.Statements[0].Command
	if cmd == nil {
		t.Fatal("expected Command not nil")
	}
	if cmd.Action != "coordinate" {
		t.Errorf("action: got %q want coordinate", cmd.Action)
	}
	if cmd.Arg != "smoke" {
		t.Errorf("name: got %q want smoke", cmd.Arg)
	}

	// Body content sanity — the things that matter for downstream parsing
	body := cmd.Arg2
	checks := []struct {
		name  string
		found bool
	}{
		{"coordination directive", strings.Contains(body, "coordination blackboard")},
		{"convergence directive", strings.Contains(body, "convergence  state_equilibrium")},
		{"agent declaration", strings.Contains(body, `agent "participant"`)},
		{"system prompt with quotes", strings.Contains(body, `system "You participate. Respond with empty writes."`)},
		{"backtick pattern", strings.Contains(body, "`_`")},
		{"subscribe block", strings.Contains(body, "subscribe (")},
	}
	for _, c := range checks {
		if !c.found {
			t.Errorf("body missing %s", c.name)
		}
	}
}

// TestConvergenceDirectiveInsideCoordinate is a focused regression test
// for the word-boundary bug: inside a coordinate block, the line
// `convergence state_equilibrium` must NOT be treated as a converge
// block opener.
func TestConvergenceDirectiveInsideCoordinate(t *testing.T) {
	input := `coordinate "x" (
  coordination blackboard
  convergence state_equilibrium
  agent "a" claude (
    system "s"
    subscribe ( on _ matching ` + "`_`" + ` => "noop" )
  )
)`

	s1 := preprocessConverge(input)

	// Critical: preprocessConverge must NOT have rewritten anything,
	// because there's no actual `converge "..."` block in the input.
	// The input should come out unchanged (modulo whitespace).
	if strings.Contains(s1, `converge "" "`) || strings.Contains(s1, `converge "" """`) {
		t.Errorf("preprocessConverge incorrectly matched 'convergence':\noutput:\n%s", s1)
	}
}

// TestTripleQuoteConvergeRoundtrip — same sanity for converge itself.
func TestTripleQuoteConvergeRoundtrip(t *testing.T) {
	input := `converge "demo" (
  context (
    sandbox "/tmp/x"
    session claude
  )
  validate (
    exec "go test ./..."
  )
)`

	s1 := preprocessConverge(input)
	if !strings.Contains(s1, `converge "demo" """`) {
		t.Errorf("expected triple-quoted converge, got:\n%s", s1)
	}

	prog, err := Parse(s1)
	if err != nil {
		t.Fatalf("parse failed: %v\n\ninput:\n%s", err, s1)
	}
	cmd := prog.Statements[0].Command
	if cmd.Action != "converge" {
		t.Errorf("action: got %q", cmd.Action)
	}
	if cmd.Arg != "demo" {
		t.Errorf("name: got %q", cmd.Arg)
	}
	if !strings.Contains(cmd.Arg2, `exec "go test ./..."`) {
		t.Errorf("body missing exec line:\n%s", cmd.Arg2)
	}
}
