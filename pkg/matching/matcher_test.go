package matching

import (
	"fmt"
	"reflect"
	"testing"
)

// patternOrPanic compiles a pattern, panicking on parse error. Used
// in table-driven tests to keep the table concise.
func patternOrPanic(src string) Pattern {
	p, err := Parse(src)
	if err != nil {
		panic(fmt.Sprintf("failed to parse %q: %v", src, err))
	}
	return p
}

func TestLiteralMatching(t *testing.T) {
	tests := []struct {
		pattern string
		value   Value
		match   bool
	}{
		{`"billing"`, "billing", true},
		{`"billing"`, "technical", false},
		{`42`, 42.0, true},
		{`42`, 43.0, false},
		{`true`, true, true},
		{`true`, false, false},
		{`null`, nil, true},
		{`null`, "something", false},
		{`"billing"`, 42.0, false},
	}
	for _, tc := range tests {
		t.Run(tc.pattern+"_vs_"+fmt.Sprintf("%v", tc.value), func(t *testing.T) {
			pat := patternOrPanic(tc.pattern)
			_, ok, err := Match(pat, tc.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.match {
				t.Errorf("pattern %q vs %v: got match=%v, want %v", tc.pattern, tc.value, ok, tc.match)
			}
		})
	}
}

func TestWildcardMatching(t *testing.T) {
	tests := []Value{
		"anything",
		42.0,
		true,
		nil,
		map[string]interface{}{"x": 1},
		[]interface{}{1, 2, 3},
	}
	pat := patternOrPanic("_")
	for _, v := range tests {
		t.Run(fmt.Sprintf("%v", v), func(t *testing.T) {
			_, ok, err := Match(pat, v)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if !ok {
				t.Errorf("wildcard should match %v", v)
			}
		})
	}
}

func TestVariableBinding(t *testing.T) {
	pat := patternOrPanic("$x")
	b, ok, err := Match(pat, "hello")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !ok {
		t.Fatal("expected match")
	}
	if b["x"] != "hello" {
		t.Errorf("expected binding x=hello, got %v", b)
	}
}

func TestObjectMatching(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   Value
		match   bool
		checks  map[string]Value
	}{
		{
			name:    "exact match",
			pattern: `{category: "billing"}`,
			value:   map[string]interface{}{"category": "billing"},
			match:   true,
		},
		{
			name:    "extra fields OK",
			pattern: `{category: "billing"}`,
			value:   map[string]interface{}{"category": "billing", "urgency": "high"},
			match:   true,
		},
		{
			name:    "missing field fails",
			pattern: `{category: "billing", urgency: "high"}`,
			value:   map[string]interface{}{"category": "billing"},
			match:   false,
		},
		{
			name:    "wrong value fails",
			pattern: `{category: "billing"}`,
			value:   map[string]interface{}{"category": "tech"},
			match:   false,
		},
		{
			name:    "bind variable in field",
			pattern: `{category: "billing", urgency: $u}`,
			value:   map[string]interface{}{"category": "billing", "urgency": "high"},
			match:   true,
			checks:  map[string]Value{"u": "high"},
		},
		{
			name:    "shorthand binds key name",
			pattern: `{category}`,
			value:   map[string]interface{}{"category": "billing"},
			match:   true,
			checks:  map[string]Value{"category": "billing"},
		},
		{
			name:    "nested object",
			pattern: `{outer: {inner: $val}}`,
			value:   map[string]interface{}{"outer": map[string]interface{}{"inner": 42.0}},
			match:   true,
			checks:  map[string]Value{"val": 42.0},
		},
		{
			name:    "non-object value",
			pattern: `{x: 1}`,
			value:   "string",
			match:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pat := patternOrPanic(tc.pattern)
			b, ok, err := Match(pat, tc.value)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if ok != tc.match {
				t.Errorf("match: got %v want %v (pattern=%q, value=%v)", ok, tc.match, tc.pattern, tc.value)
				return
			}
			if ok {
				for k, expected := range tc.checks {
					if !valuesEqual(b[k], expected) {
						t.Errorf("binding %s: got %v want %v", k, b[k], expected)
					}
				}
			}
		})
	}
}

func TestListMatching(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   Value
		match   bool
		checks  map[string]Value
	}{
		{
			name:    "fixed length match",
			pattern: `[1, 2, 3]`,
			value:   []interface{}{1.0, 2.0, 3.0},
			match:   true,
		},
		{
			name:    "fixed length mismatch",
			pattern: `[1, 2, 3]`,
			value:   []interface{}{1.0, 2.0},
			match:   false,
		},
		{
			name:    "fixed length wrong value",
			pattern: `[1, 2, 3]`,
			value:   []interface{}{1.0, 2.0, 4.0},
			match:   false,
		},
		{
			name:    "head and tail",
			pattern: `[$first, ...$rest]`,
			value:   []interface{}{1.0, 2.0, 3.0, 4.0},
			match:   true,
			checks:  map[string]Value{"first": 1.0},
		},
		{
			name:    "head and tail with empty tail",
			pattern: `[$first, ...$rest]`,
			value:   []interface{}{1.0},
			match:   true,
			checks:  map[string]Value{"first": 1.0},
		},
		{
			name:    "just tail",
			pattern: `[...$all]`,
			value:   []interface{}{1.0, 2.0, 3.0},
			match:   true,
		},
		{
			name:    "head longer than value",
			pattern: `[$a, $b]`,
			value:   []interface{}{1.0},
			match:   false,
		},
		{
			name:    "non-list value",
			pattern: `[1, 2]`,
			value:   "not a list",
			match:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pat := patternOrPanic(tc.pattern)
			b, ok, err := Match(pat, tc.value)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if ok != tc.match {
				t.Errorf("match: got %v want %v", ok, tc.match)
				return
			}
			if ok {
				for k, expected := range tc.checks {
					if !valuesEqual(b[k], expected) {
						t.Errorf("binding %s: got %v want %v", k, b[k], expected)
					}
				}
			}
		})
	}
}

func TestListTailValues(t *testing.T) {
	pat := patternOrPanic(`[$first, ...$rest]`)
	b, ok, err := Match(pat, []interface{}{10.0, 20.0, 30.0})
	if err != nil || !ok {
		t.Fatalf("expected match: ok=%v err=%v", ok, err)
	}
	expectedRest := []interface{}{20.0, 30.0}
	if !reflect.DeepEqual(b["rest"], expectedRest) {
		t.Errorf("tail: got %v want %v", b["rest"], expectedRest)
	}
}

func TestConsistentBindings(t *testing.T) {
	// Same variable bound twice must match the same value
	pat := patternOrPanic(`{a: $x, b: $x}`)
	_, ok, err := Match(pat, map[string]interface{}{"a": 1.0, "b": 1.0})
	if err != nil || !ok {
		t.Errorf("expected match when x=x: ok=%v err=%v", ok, err)
	}
	_, ok, err = Match(pat, map[string]interface{}{"a": 1.0, "b": 2.0})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Errorf("expected no match when x != x")
	}
}

func TestGuardEvaluation(t *testing.T) {
	pat, err := Parse(`{count: $n} when n > 5`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Plug in a simple guard evaluator for testing
	pat.Guard.EvalFunc = func(b Bindings) (bool, error) {
		n, ok := b["n"].(float64)
		if !ok {
			return false, fmt.Errorf("n not a number")
		}
		return n > 5, nil
	}

	_, ok, err := Match(pat, map[string]interface{}{"count": 10.0})
	if err != nil || !ok {
		t.Errorf("expected match for count=10: ok=%v err=%v", ok, err)
	}
	_, ok, err = Match(pat, map[string]interface{}{"count": 3.0})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Errorf("expected no match for count=3")
	}
}

func TestRealWorldClassifierExample(t *testing.T) {
	// Simulate an agent classifier output → dispatch by category
	tests := []struct {
		name  string
		value Value
		arm   int
	}{
		{
			name:  "billing high urgency",
			value: MustParseJSON(`{"category": "billing", "urgency": "high"}`),
			arm:   1,
		},
		{
			name:  "billing normal",
			value: MustParseJSON(`{"category": "billing", "urgency": "normal"}`),
			arm:   2,
		},
		{
			name:  "technical with product",
			value: MustParseJSON(`{"category": "technical", "product": "widget"}`),
			arm:   3,
		},
		{
			name:  "unknown category",
			value: MustParseJSON(`{"category": "other"}`),
			arm:   4,
		},
	}

	arms := []struct {
		pat string
	}{
		{`{category: "billing", urgency: "high"}`}, // arm 1
		{`{category: "billing"}`},                  // arm 2
		{`{category: "technical", product: $p}`},   // arm 3
		{`_`},                                      // arm 4 (catchall)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Dispatch: first matching arm wins
			matched := 0
			for i, a := range arms {
				p := patternOrPanic(a.pat)
				_, ok, err := Match(p, tc.value)
				if err != nil {
					t.Fatalf("err: %v", err)
				}
				if ok {
					matched = i + 1
					break
				}
			}
			if matched != tc.arm {
				t.Errorf("got arm %d, want %d", matched, tc.arm)
			}
		})
	}
}
