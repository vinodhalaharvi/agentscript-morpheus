package exec

import (
	"context"
	"strings"
	"testing"
)

func TestExecBasic(t *testing.T) {
	p := NewPlugin(false)
	cmds := p.Commands()
	fn, ok := cmds["exec"]
	if !ok {
		t.Fatal("exec command not registered")
	}

	out, err := fn(context.Background(), []string{"echo hello"}, "")
	if err != nil {
		t.Fatalf("exec echo failed: %v", err)
	}
	if out != "hello" {
		t.Errorf("want 'hello', got %q", out)
	}
}

func TestExecUsesPipedInputAsStdin(t *testing.T) {
	p := NewPlugin(false)
	fn := p.Commands()["exec"]

	// Pipe 3 newline-terminated lines into `wc -l` → expect 3
	out, err := fn(context.Background(), []string{"wc -l"}, "foo\nbar\nbaz\n")
	if err != nil {
		t.Fatalf("exec wc failed: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(out), "3") {
		t.Errorf("expected line count 3, got %q", out)
	}
}

func TestExecFailsOnNonZeroExit(t *testing.T) {
	p := NewPlugin(false)
	fn := p.Commands()["exec"]

	_, err := fn(context.Background(), []string{"exit 7"}, "")
	if err == nil {
		t.Fatal("expected error on exit 7, got nil")
	}
	if !strings.Contains(err.Error(), "exited 7") {
		t.Errorf("expected exit code in error, got: %v", err)
	}
}

func TestExecEmptyCommandErrors(t *testing.T) {
	p := NewPlugin(false)
	fn := p.Commands()["exec"]

	_, err := fn(context.Background(), []string{}, "")
	if err == nil {
		t.Fatal("expected error on empty command, got nil")
	}
}

func TestExecUsesPipedInputAsCommandWhenArgEmpty(t *testing.T) {
	p := NewPlugin(false)
	fn := p.Commands()["exec"]

	// No args — piped input becomes the command
	out, err := fn(context.Background(), []string{}, "echo from-input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "from-input" {
		t.Errorf("want 'from-input', got %q", out)
	}
}
