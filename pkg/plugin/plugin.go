// Package plugin defines the seam between the AgentScript runtime and
// any capability package. It is the only package both internal/agentscript
// and pkg/* need to agree on.
//
// The three mechanisms at work here:
//   - CommandFunc  — function type (behavior as value)
//   - Plugin       — interface (the contract / seam)
//   - Registry     — composes plugins into a dispatchable map
//
// Generics live one level down, inside plugin implementations and
// infrastructure packages (cache, retry) — not at this boundary,
// because the DSL pipeline is string-typed by design.
package plugin

import (
	"context"
	"fmt"
	"sort"
)

// CommandFunc is the core Kleisli arrow: (ctx, args, input) → (string, error).
// Every DSL command is exactly this shape. Context carries the Reader monad
// (cancellation, deadlines, values). Args map to Arg/Arg2/Arg3 from the grammar.
// Input is the piped value from the previous pipeline stage.
type CommandFunc func(ctx context.Context, args []string, input string) (string, error)

// Plugin is the seam. Any pkg that exposes DSL commands implements this.
// Name() is used for logging and debugging only — commands are keyed by
// the strings in Commands(), not by Name().
type Plugin interface {
	Name() string
	Commands() map[string]CommandFunc
}

// Registry dispatches named commands to their CommandFunc.
// It knows nothing about what the commands do — only that they exist.
type Registry struct {
	commands map[string]CommandFunc
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]CommandFunc)}
}

// Register adds all commands from a Plugin.
// Later registrations overwrite earlier ones — plugins compose by replacement,
// which lets you swap implementations without touching the registry call site.
func (r *Registry) Register(p Plugin) {
	for name, fn := range p.Commands() {
		r.commands[name] = fn
	}
}

// RegisterFunc registers a single named CommandFunc directly.
// Use for one-off commands that don't warrant a full Plugin struct.
func (r *Registry) RegisterFunc(name string, fn CommandFunc) {
	r.commands[name] = fn
}

// Execute dispatches a command by name.
// Returns (result, true, nil/err) if found.
// Returns ("", false, nil) if not found — caller decides the fallback.
func (r *Registry) Execute(ctx context.Context, name string, args []string, input string) (string, bool, error) {
	fn, ok := r.commands[name]
	if !ok {
		return "", false, nil
	}
	result, err := fn(ctx, args, input)
	return result, true, err
}

// Has returns true if a command is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.commands[name]
	return ok
}

// Names returns all registered command names in sorted order.
// Useful for documentation, help text, and debugging.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Arg safely extracts args[i], returning "" if out of bounds.
// Keeps plugin implementations clean — no index-out-of-range panics.
func Arg(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// RequireArg extracts args[i] or returns a descriptive error.
// Use when the argument is mandatory for the command to function.
func RequireArg(args []string, i int, name string) (string, error) {
	if i < len(args) && args[i] != "" {
		return args[i], nil
	}
	return "", fmt.Errorf("missing required argument %q (position %d)", name, i)
}

// Coalesce returns the first non-empty string from args[i] or fallback.
// Common pattern: use arg if provided, else use piped input.
func Coalesce(args []string, i int, fallback string) string {
	if v := Arg(args, i); v != "" {
		return v
	}
	return fallback
}
