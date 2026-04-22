// Package exec provides a top-level shell command for AgentScript pipelines.
//
// The exec command runs a shell command via `sh -c` and returns stdout on
// success (empty args OK — piped input becomes stdin). Non-zero exit fails
// the pipeline with stderr as the error message. This is the primitive you
// want for git clone / go build / go test / buf generate / anything that
// lives outside a converge block but needs to happen mid-pipeline.
//
// Naming: "exec" collides with the line-prefix inside converge validate()
// blocks, but those are parsed by pkg/intent/parser.go and never reach the
// main DSL lexer — different codepath, no runtime conflict. Semantically
// both mean "run a shell command", so the shared name is consistent.
package exec

import (
	"context"
	"fmt"
	osexec "os/exec"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin exposes the `exec` command.
type Plugin struct {
	verbose bool
}

// NewPlugin creates a new exec plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{verbose: verbose}
}

// Name returns the plugin name (used for logs/debug only).
func (p *Plugin) Name() string { return "exec" }

// Commands registers the `exec` command with the runtime.
func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"exec": p.run,
	}
}

// run executes the command string via `sh -c`.
//
// Argument precedence:
//  1. args[0] ("the command to run")
//  2. piped input (for cases like:  ask "what should we run" >=> exec)
//
// Piped input is ALSO forwarded to the child process as stdin, so you can
// use exec at both ends of a pipe:
//
//	exec "cat README.md" >=> exec "wc -l"
//
// Exit-code semantics:
//   - exit 0  → return stdout (stderr discarded unless verbose)
//   - exit !0 → return an error containing stderr + stdout
//
// Failure short-circuits the pipeline — same as every other plugin command.
func (p *Plugin) run(ctx context.Context, args []string, input string) (string, error) {
	cmd := plugin.Coalesce(args, 0, input)
	if strings.TrimSpace(cmd) == "" {
		return "", fmt.Errorf(`exec: no command — usage: exec "go build ./..."`)
	}

	if p.verbose {
		fmt.Printf("   🖥️  exec: %s\n", cmd)
	}

	c := osexec.CommandContext(ctx, "sh", "-c", cmd)

	// Forward piped input as stdin only when the command came from args.
	// If the command came from input (the Coalesce fallback), feeding
	// input as stdin would be a double-use and usually wrong.
	if plugin.Arg(args, 0) != "" && input != "" {
		c.Stdin = strings.NewReader(input)
	}

	// CombinedOutput mingles stdout+stderr — fine for shell workflows where
	// users want to see warnings alongside results. If a user specifically
	// needs clean stdout (e.g. for piping to another exec), they can
	// redirect stderr in the command itself: `exec "go build ./... 2>/dev/null"`.
	out, err := c.CombinedOutput()
	outStr := strings.TrimRight(string(out), "\n")

	if err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			return "", fmt.Errorf("exec %q exited %d: %s", cmd, exitErr.ExitCode(), outStr)
		}
		return "", fmt.Errorf("exec %q failed: %w (%s)", cmd, err, outStr)
	}

	return outStr, nil
}
