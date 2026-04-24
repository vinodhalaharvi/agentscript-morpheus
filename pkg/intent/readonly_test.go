package intent

import (
	"strings"
	"testing"
)

func TestIsReadOnly(t *testing.T) {
	tests := []struct {
		name     string
		prefixes []string
		path     string
		want     bool
	}{
		{"direct hit under gen", []string{"gen/"}, "gen/service/v1/task.pb.go", true},
		{"exact match", []string{"gen"}, "gen", true},
		{"no prefix configured", nil, "gen/task.pb.go", false},
		{"different directory", []string{"gen/"}, "internal/service/x.go", false},
		{"similar-but-different prefix should NOT match", []string{"gen"}, "generator/x.go", false},
		{"trailing-slash prefix same as no-slash", []string{"gen/"}, "gen/x.go", true},
		{"no-slash prefix same as trailing-slash", []string{"gen"}, "gen/x.go", true},
		{"multiple prefixes, first hits", []string{"gen", "vendor"}, "gen/x.go", true},
		{"multiple prefixes, second hits", []string{"gen", "vendor"}, "vendor/mod/x.go", true},
		{"multiple prefixes, none hit", []string{"gen", "vendor"}, "internal/x.go", false},
		{"leading ./ on path", []string{"gen"}, "./gen/x.go", true},
		{"leading ./ on prefix", []string{"./gen"}, "gen/x.go", true},
		{"empty string prefix is ignored", []string{""}, "gen/x.go", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{config: Config{ReadOnlyPaths: tc.prefixes}}
			if got := e.isReadOnly(tc.path); got != tc.want {
				t.Errorf("isReadOnly(%q) with prefixes %v = %v, want %v",
					tc.path, tc.prefixes, got, tc.want)
			}
		})
	}
}

// TestRunAbortsOnPipelineError verifies the fail-fast behaviour: when the
// pipeline DSL returns an error, Run() must NOT fall through to the intent
// loop. Historical bug: the engine would log "⚠️ Pipeline error" and then
// happily validate whatever stale state was on disk, producing fake
// "intent satisfied" messages.
func TestRunAbortsOnPipelineError(t *testing.T) {
	// A reasoner that should never be invoked — if validation runs, we
	// fail the test.
	reasoner := func(prompt string) (string, error) {
		t.Fatal("reasoner invoked — intent loop should NOT have run after pipeline failure")
		return "", nil
	}

	// runDSL returns an error, simulating a parse failure or exec crash.
	runDSL := func(dsl string) (string, error) {
		return "", &syntheticErr{"simulated pipeline crash"}
	}

	sb, err := NewSandbox(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox init: %v", err)
	}

	e := &Engine{
		config: Config{
			Sandbox:      sb.Root,
			MaxRetries:   5,
			RetryDelay:   1,
			Mode:         "propose",
			PipelineDSL:  "exec 'some command that fails to parse'",
			ValidateCmds: []string{"true"},
		},
		reasoner: reasoner,
		runDSL:   runDSL,
		history:  make([]HistoryEntry, 0),
		sandbox:  sb,
	}

	err = e.Run()
	if err == nil {
		t.Fatal("Run() returned nil after pipeline error — should have failed")
	}
	if !strings.Contains(err.Error(), "pipeline failed") {
		t.Errorf("error should mention 'pipeline failed', got: %v", err)
	}
}

type syntheticErr struct{ msg string }

func (e *syntheticErr) Error() string { return e.msg }
