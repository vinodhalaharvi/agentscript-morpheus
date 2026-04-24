package coordinate

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/matching"
)

// Config holds a parsed coordinate block definition, ready for the engine
// to instantiate.
//
// Parser input is the decoded body of a coordinate block — the text
// between the outermost ( ) after preprocessing turned newlines back
// from the ||| encoding. Typical input:
//
//	coordination blackboard
//	convergence  state_equilibrium
//
//	context (
//	  session claude
//	  stability_rounds 3
//	  max_rounds 30
//	)
//
//	blackboard (
//	  write_policy higher_confidence_wins
//	)
//
//	agent "vocabulary-expert" claude (
//	  system "You know unusual and rare words."
//	  subscribe (
//	    on cell_changed matching `{confidence: $c} when $c > 0.9` =>
//	      "propagate constraints to my clues"
//	  )
//	)
//
// Grammar is line-oriented at the top level; nested blocks delimited by ( ).
type Config struct {
	Name         string
	Coordination string
	Convergence  string

	// Context params
	Sandbox            string
	Session            string // "claude" | "" (none = default single-shot)
	StabilityRounds    int
	MaxRounds          int
	AlignmentThreshold float64
	Quorum             string

	// Blackboard options
	WritePolicy string // "last_write_wins" | "higher_confidence_wins" | "append_only"

	// Agents — Phase 1 supports blackboard agents only
	Agents []AgentConfig

	// Goal predicate expression (for future blackboard+goal_completion)
	GoalExpr string
}

// AgentConfig is the parsed form of an agent block.
type AgentConfig struct {
	ID            string
	Reasoner      string // "claude" for Phase 1
	System        string
	Subscriptions []SubscriptionConfig
}

// SubscriptionConfig describes a `subscribe (on X => instruction)` entry.
type SubscriptionConfig struct {
	// KeyFilterGlob optional — e.g. "cells/*" to filter events by key
	KeyFilterGlob string
	// PatternSrc is the raw pattern text (compiled at engine start)
	PatternSrc string
	// Pattern is the compiled pattern
	Pattern matching.Pattern
	// Instruction is the task the agent performs when the subscription fires
	Instruction string
}

// ParseCoordinateBody parses a decoded coordinate body into a Config.
// The name is passed separately because the preprocessor already extracted it.
func ParseCoordinateBody(name, body string) (*Config, error) {
	cfg := &Config{
		Name:            name,
		StabilityRounds: 3,
		MaxRounds:       20,
		Session:         "claude",
		WritePolicy:     "last_write_wins",
	}

	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		// Skip blank lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			i++
			continue
		}

		// coordination <id>
		if strings.HasPrefix(trimmed, "coordination ") {
			cfg.Coordination = strings.TrimSpace(strings.TrimPrefix(trimmed, "coordination "))
			i++
			continue
		}

		// convergence <id>
		if strings.HasPrefix(trimmed, "convergence ") {
			cfg.Convergence = strings.TrimSpace(strings.TrimPrefix(trimmed, "convergence "))
			i++
			continue
		}

		// context ( ... )
		if strings.HasPrefix(trimmed, "context") && strings.Contains(trimmed, "(") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("context block: %w", err)
			}
			if err := cfg.parseContextBlock(block); err != nil {
				return nil, err
			}
			i = end + 1
			continue
		}

		// blackboard ( ... )
		if strings.HasPrefix(trimmed, "blackboard") && strings.Contains(trimmed, "(") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("blackboard block: %w", err)
			}
			if err := cfg.parseBlackboardBlock(block); err != nil {
				return nil, err
			}
			i = end + 1
			continue
		}

		// agent "name" claude ( ... )
		if strings.HasPrefix(trimmed, "agent ") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("agent block: %w", err)
			}
			agent, err := parseAgentHeader(trimmed)
			if err != nil {
				return nil, err
			}
			if err := parseAgentBlock(block, agent); err != nil {
				return nil, err
			}
			cfg.Agents = append(cfg.Agents, *agent)
			i = end + 1
			continue
		}

		// goal ( ... ) — stashed for future use
		if strings.HasPrefix(trimmed, "goal") && strings.Contains(trimmed, "(") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("goal block: %w", err)
			}
			cfg.GoalExpr = block
			i = end + 1
			continue
		}

		// equilibrium ( ... ), belief_alignment ( ... ), witness { ... } — skip for Phase 1
		// (recognized but not fully processed — prevents parse errors on valid syntax)
		for _, kw := range []string{"equilibrium", "belief_alignment", "consensus"} {
			if strings.HasPrefix(trimmed, kw) && strings.Contains(trimmed, "(") {
				_, end, err := extractBlock(lines, i)
				if err != nil {
					return nil, fmt.Errorf("%s block: %w", kw, err)
				}
				i = end + 1
				goto nextLine
			}
		}
		if strings.HasPrefix(trimmed, "witness") {
			// witness { ... } — eat to matching }
			_, end, err := extractBraceBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("witness block: %w", err)
			}
			i = end + 1
			continue
		}

		// Unknown line — for v1 we just skip with a warning rather than fail.
		// Strict mode can be added later.
		i++

	nextLine:
	}

	// Validation
	if cfg.Coordination == "" {
		return nil, fmt.Errorf("missing `coordination <model>` declaration")
	}
	if cfg.Convergence == "" {
		return nil, fmt.Errorf("missing `convergence <notion>` declaration")
	}
	if err := ValidateCompatibility(cfg.Coordination, cfg.Convergence); err != nil {
		return nil, err
	}
	if len(cfg.Agents) == 0 {
		return nil, fmt.Errorf("coordinate block requires at least one agent")
	}

	// Compile all patterns now so errors surface at parse time, not at event time
	for i := range cfg.Agents {
		for j := range cfg.Agents[i].Subscriptions {
			s := &cfg.Agents[i].Subscriptions[j]
			p, err := matching.Parse(s.PatternSrc)
			if err != nil {
				return nil, fmt.Errorf("agent %s subscription %d: bad pattern %q: %w",
					cfg.Agents[i].ID, j, s.PatternSrc, err)
			}
			s.Pattern = p
		}
	}

	return cfg, nil
}

// parseContextBlock reads `key value` lines from the context body.
func (cfg *Config) parseContextBlock(body string) error {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		key := parts[0]
		val := strings.TrimSpace(parts[1])
		val = unquote(val)

		switch key {
		case "sandbox":
			cfg.Sandbox = val
		case "session":
			cfg.Session = val
		case "stability_rounds":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.StabilityRounds = n
			}
		case "max_rounds":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.MaxRounds = n
			}
		case "alignment_threshold":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				cfg.AlignmentThreshold = f
			}
		case "quorum":
			cfg.Quorum = val
		}
	}
	return nil
}

// parseBlackboardBlock reads the `write_policy`, schema, etc.
func (cfg *Config) parseBlackboardBlock(body string) error {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "write_policy ") {
			cfg.WritePolicy = strings.TrimSpace(strings.TrimPrefix(line, "write_policy "))
		}
		// schema { ... } is not enforced in Phase 1 — blackboard is untyped.
	}
	return nil
}

// parseAgentHeader parses the opening line: agent "name" claude (
func parseAgentHeader(line string) (*AgentConfig, error) {
	// Strip leading "agent "
	rest := strings.TrimSpace(strings.TrimPrefix(line, "agent "))
	// Next token is quoted name
	if !strings.HasPrefix(rest, `"`) {
		return nil, fmt.Errorf("agent header: expected quoted name, got %q", rest)
	}
	closeIdx := strings.Index(rest[1:], `"`)
	if closeIdx < 0 {
		return nil, fmt.Errorf("agent header: unterminated name")
	}
	id := rest[1 : 1+closeIdx]
	rest = strings.TrimSpace(rest[1+closeIdx+1:])
	// Next token is reasoner
	parenIdx := strings.Index(rest, "(")
	if parenIdx < 0 {
		return nil, fmt.Errorf("agent header: missing '('")
	}
	reasoner := strings.TrimSpace(rest[:parenIdx])
	if reasoner == "" {
		reasoner = "claude"
	}
	return &AgentConfig{ID: id, Reasoner: reasoner}, nil
}

// parseAgentBlock reads the body of an agent ( ... ) block.
// Supports:
//
//	system "..."
//	subscribe ( ... )
func parseAgentBlock(body string, agent *AgentConfig) error {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "system ") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "system "))
			agent.System = unquote(val)
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "subscribe") && strings.Contains(trimmed, "(") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return fmt.Errorf("subscribe block: %w", err)
			}
			subs, err := parseSubscribeBlock(block)
			if err != nil {
				return err
			}
			agent.Subscriptions = append(agent.Subscriptions, subs...)
			i = end + 1
			continue
		}
		i++
	}
	return nil
}

// parseSubscribeBlock parses entries like:
//
//	on cells/* matching `{letter: $l, confidence: $c} when $c > 0.9` =>
//	  "react to high-confidence cell updates in my area"
//
// Each entry spans possibly multiple lines; we use =>  as the separator
// between the match spec and the instruction.
func parseSubscribeBlock(body string) ([]SubscriptionConfig, error) {
	var subs []SubscriptionConfig

	// Flatten body into one string; semantically subscriptions are whitespace-delimited.
	// We use a simple state machine: look for `on ... => "instruction"`.
	text := body
	for {
		onIdx := strings.Index(text, "on ")
		if onIdx < 0 {
			break
		}
		arrowIdx := strings.Index(text[onIdx:], "=>")
		if arrowIdx < 0 {
			return nil, fmt.Errorf("subscription missing '=>': %s", text[onIdx:])
		}
		matchSpec := strings.TrimSpace(text[onIdx+3 : onIdx+arrowIdx])

		// Find the instruction — a quoted string after =>
		rest := text[onIdx+arrowIdx+2:]
		instruction, consumedTo, err := extractQuotedInstruction(rest)
		if err != nil {
			return nil, fmt.Errorf("subscription instruction: %w", err)
		}

		sub, err := parseMatchSpec(matchSpec)
		if err != nil {
			return nil, err
		}
		sub.Instruction = instruction
		subs = append(subs, sub)

		text = rest[consumedTo:]
	}
	return subs, nil
}

// extractQuotedInstruction pulls a "..." or `...` string from the start of s.
// Returns the raw string contents, the byte position where consumption ended.
func extractQuotedInstruction(s string) (string, int, error) {
	// Skip leading whitespace
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	if i >= len(s) {
		return "", 0, fmt.Errorf("expected instruction string")
	}
	quote := s[i]
	if quote != '"' && quote != '`' {
		return "", 0, fmt.Errorf("expected '\"' or '`', got %q", s[i])
	}
	i++
	start := i
	for i < len(s) && s[i] != quote {
		if s[i] == '\\' && i+1 < len(s) && quote == '"' {
			i += 2
			continue
		}
		i++
	}
	if i >= len(s) {
		return "", 0, fmt.Errorf("unterminated instruction string")
	}
	return s[start:i], i + 1, nil
}

// parseMatchSpec parses the "on" target:
//
//	<keyglob> matching `<pattern>`
//	<keyglob>                         (no pattern — always matches any value)
//	matching `<pattern>`              (no keyglob — matches any key)
//	`<pattern>`                       (shorthand, same as above)
func parseMatchSpec(spec string) (SubscriptionConfig, error) {
	spec = strings.TrimSpace(spec)
	var s SubscriptionConfig

	// Look for "matching"
	if idx := strings.Index(spec, "matching"); idx >= 0 {
		keyPart := strings.TrimSpace(spec[:idx])
		if keyPart != "" {
			s.KeyFilterGlob = keyPart
		}
		// After "matching" — expect a backtick-wrapped pattern
		rest := strings.TrimSpace(spec[idx+len("matching"):])
		if len(rest) < 2 || rest[0] != '`' {
			return s, fmt.Errorf("expected `pattern` after 'matching', got: %q", rest)
		}
		close := strings.Index(rest[1:], "`")
		if close < 0 {
			return s, fmt.Errorf("unterminated pattern: %q", rest)
		}
		s.PatternSrc = rest[1 : 1+close]
		return s, nil
	}

	// No "matching" — treat whole spec as key filter; pattern defaults to wildcard
	if spec != "" {
		s.KeyFilterGlob = spec
	}
	s.PatternSrc = "_"
	return s, nil
}

// ============================================================
// Block extraction — small utilities, similar to pkg/intent/parser.go
// ============================================================

// extractBlock finds the ( ... ) block starting at lines[startLine].
// Returns the body (text between the outermost parens), the index of
// the closing ')' line, and error.
func extractBlock(lines []string, startLine int) (string, int, error) {
	var sb strings.Builder
	depth := 0
	started := false
	inDouble, inSingle, inBacktick := false, false, false

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		lineStart := sb.Len()

		for k := 0; k < len(line); k++ {
			c := line[k]
			inQ := inDouble || inSingle || inBacktick

			if !inQ {
				switch c {
				case '"':
					inDouble = true
				case '\'':
					inSingle = true
				case '`':
					inBacktick = true
				}
			} else {
				if c == '"' && inDouble && (k == 0 || line[k-1] != '\\') {
					inDouble = false
				} else if c == '\'' && inSingle && (k == 0 || line[k-1] != '\\') {
					inSingle = false
				} else if c == '`' && inBacktick {
					inBacktick = false
				}
			}

			if !inDouble && !inSingle && !inBacktick {
				if c == '(' {
					if !started {
						started = true
						depth = 1
						continue
					}
					depth++
				} else if c == ')' && started {
					depth--
					if depth == 0 {
						return strings.TrimSpace(sb.String()), i, nil
					}
				}
			}
			if started && depth > 0 {
				sb.WriteByte(c)
			}
		}
		if started && depth > 0 {
			// Add a newline separator between lines (only if we wrote anything this line)
			if sb.Len() > lineStart {
				sb.WriteByte('\n')
			}
		}
	}
	return "", 0, fmt.Errorf("unterminated block starting at line %d", startLine+1)
}

// extractBraceBlock is similar but for { ... }.
func extractBraceBlock(lines []string, startLine int) (string, int, error) {
	var sb strings.Builder
	depth := 0
	started := false

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		for k := 0; k < len(line); k++ {
			c := line[k]
			if c == '{' {
				if !started {
					started = true
					depth = 1
					continue
				}
				depth++
			} else if c == '}' && started {
				depth--
				if depth == 0 {
					return strings.TrimSpace(sb.String()), i, nil
				}
			}
			if started && depth > 0 {
				sb.WriteByte(c)
			}
		}
		if started && depth > 0 {
			sb.WriteByte('\n')
		}
	}
	return "", 0, fmt.Errorf("unterminated brace block starting at line %d", startLine+1)
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '`' && last == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
