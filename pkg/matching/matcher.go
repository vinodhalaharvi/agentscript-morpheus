// Package matching provides a pattern matching primitive for dispatching
// on structured values (typically decoded JSON from agent outputs or
// blackboard events).
//
// The pattern language supports:
//
//	literals:    "billing", 42, true, null
//	wildcards:   _
//	variables:   $name           — binds the matched value to name
//	objects:     {key: pattern}  — each key must match; extras ignored
//	lists:       [p1, p2, ...]   — fixed-length match
//	lists with tail: [p1, ...$rest] — binds remaining to rest
//	guards:      pattern when <expr> — expr is a bool in bindings scope
//
// The matcher returns (bindings, true) on match, (nil, false) on miss.
// Bindings are returned as map[string]Value; callers can use them to
// parameterize downstream dispatch.
//
// This primitive is used by:
//   - pkg/coordinate for subscription dispatch and merge-outcome dispatch
//   - (future) pkg/intent for match-driven converge intents
package matching

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Value represents an arbitrary matched or matchable value. In practice
// it's typically a Go type decoded from JSON (map[string]interface{},
// []interface{}, string, float64, bool, nil).
type Value = interface{}

// Bindings holds variables captured during a successful match.
type Bindings map[string]Value

// Pattern is the AST of a compiled match pattern.
type Pattern struct {
	// Exactly one of these is set.
	Literal  *LiteralPat
	Wildcard *WildcardPat
	Variable *VariablePat
	Object   *ObjectPat
	List     *ListPat

	// Guard, if present, must evaluate true for the pattern to match.
	Guard *Guard
}

// LiteralPat matches values equal to Value (string, float, bool, nil).
type LiteralPat struct {
	Value Value
}

// WildcardPat matches anything.
type WildcardPat struct{}

// VariablePat matches anything and binds the value to Name.
type VariablePat struct {
	Name string
}

// ObjectPat matches objects (map[string]interface{}).
// Every listed field must match in the object; extra fields in the
// object are ignored. A field may also be a shorthand `name` (no value)
// which is sugar for `name: $name`.
type ObjectPat struct {
	Fields []ObjectField
}

type ObjectField struct {
	Key     string
	Pattern Pattern
}

// ListPat matches arrays. If TailVar is non-empty, the head patterns
// must match the first len(Head) items and the remainder binds to
// TailVar. Without a TailVar, the array must have exactly len(Head) items.
type ListPat struct {
	Head    []Pattern
	TailVar string // if set, captures remaining items as []Value
}

// Guard is a post-match predicate evaluated in bindings scope.
// For simplicity in v1 we only support a small expression grammar
// via EvalFunc — callers supply the evaluator. This keeps the matcher
// decoupled from any specific expression language.
type Guard struct {
	Source   string                       // original text, for errors
	EvalFunc func(Bindings) (bool, error) // nil = always true
}

// Match attempts to match v against p. Returns bindings and true if
// successful. On failure returns nil and false. On a guard evaluation
// error returns the error; the caller decides how to surface it.
func Match(p Pattern, v Value) (Bindings, bool, error) {
	b := Bindings{}
	if !matchInto(p, v, b) {
		return nil, false, nil
	}
	if p.Guard != nil && p.Guard.EvalFunc != nil {
		ok, err := p.Guard.EvalFunc(b)
		if err != nil {
			return nil, false, fmt.Errorf("guard %q: %w", p.Guard.Source, err)
		}
		if !ok {
			return nil, false, nil
		}
	}
	return b, true, nil
}

// matchInto walks the pattern and value, writing bindings into b.
// Returns false if the structure doesn't match. Guards are NOT evaluated
// here — only in Match — so nested patterns can bind freely without
// triggering guards prematurely.
func matchInto(p Pattern, v Value, b Bindings) bool {
	switch {
	case p.Literal != nil:
		return valuesEqual(p.Literal.Value, v)

	case p.Wildcard != nil:
		return true

	case p.Variable != nil:
		// If the variable is already bound, enforce equality (rare but
		// supports patterns like {x: $v, y: $v} requiring x == y).
		if existing, ok := b[p.Variable.Name]; ok {
			return valuesEqual(existing, v)
		}
		b[p.Variable.Name] = v
		return true

	case p.Object != nil:
		obj, ok := v.(map[string]interface{})
		if !ok {
			return false
		}
		for _, f := range p.Object.Fields {
			fieldVal, present := obj[f.Key]
			if !present {
				return false
			}
			if !matchInto(f.Pattern, fieldVal, b) {
				return false
			}
		}
		return true

	case p.List != nil:
		arr, ok := v.([]interface{})
		if !ok {
			return false
		}
		if p.List.TailVar == "" {
			// Fixed length required
			if len(arr) != len(p.List.Head) {
				return false
			}
			for i, hp := range p.List.Head {
				if !matchInto(hp, arr[i], b) {
					return false
				}
			}
			return true
		}
		// Tail variant: head patterns consume the prefix
		if len(arr) < len(p.List.Head) {
			return false
		}
		for i, hp := range p.List.Head {
			if !matchInto(hp, arr[i], b) {
				return false
			}
		}
		// Rest goes to TailVar
		tail := arr[len(p.List.Head):]
		if existing, ok := b[p.List.TailVar]; ok {
			return valuesEqual(existing, tail)
		}
		// Make a copy so callers can't mutate internal state
		tailCopy := make([]interface{}, len(tail))
		copy(tailCopy, tail)
		b[p.List.TailVar] = tailCopy
		return true
	}
	return false
}

// ValuesEqual compares two Values structurally. Exported wrapper around
// the internal valuesEqual. Used by pkg/coordinate/blackboard for
// idempotent-write detection (writes with an unchanged value shouldn't
// reset the equilibrium clock).
func ValuesEqual(a, b Value) bool {
	return valuesEqual(a, b)
}

// valuesEqual compares two values structurally. Handles the types we
// expect from JSON-decoded data: bool, string, float64, nil, []Value,
// map[string]Value. Falls back to fmt.Sprintf comparison for exotic
// types (should be rare).
func valuesEqual(a, b Value) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case int:
		// JSON decodes all numbers as float64; support int for convenience
		if bv, ok := b.(int); ok {
			return av == bv
		}
		if bv, ok := b.(float64); ok {
			return float64(av) == bv
		}
		return false
	case []interface{}:
		bv, ok := b.([]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !valuesEqual(v, bv[k]) {
				return false
			}
		}
		return true
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// MustParseJSON is a small helper for tests and examples — parses a JSON
// string into a Value. Panics on error; don't use in production code.
func MustParseJSON(s string) Value {
	var v Value
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &v); err != nil {
		panic(fmt.Sprintf("MustParseJSON: %v", err))
	}
	return v
}
