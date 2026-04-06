package intent

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseFile parses a .as file that contains context/intent/validate blocks
// and returns the Config plus any pipeline DSL that precedes the blocks.
func ParseFile(content string) (*Config, error) {
	cfg := &Config{
		MaxRetries:   5,
		RetryDelay:   5,
		Mode:         "propose",
		HistoryCount: 3,
		Reasoner:     "claude",
	}

	lines := strings.Split(content, "\n")

	var pipelineLines []string
	i := 0

	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			if !inBlock(lines, i, "context", "intent", "validate") {
				pipelineLines = append(pipelineLines, lines[i])
			}
			i++
			continue
		}

		// Top-level converge config (before context/intent/validate)
		if parseTopLevelConfig(trimmed, cfg) {
			i++
			continue
		}

		// context (
		if strings.HasPrefix(trimmed, "context") && strings.Contains(trimmed, "(") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("context block: %w", err)
			}
			parseContextBlock(block, cfg)
			i = end + 1
			continue
		}

		// intent 5 60 propose (
		if strings.HasPrefix(trimmed, "intent") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("intent block: %w", err)
			}
			parseIntentHeader(trimmed, cfg)
			parseIntentBlock(block, cfg)
			i = end + 1
			continue
		}

		// validate (
		if strings.HasPrefix(trimmed, "validate") && strings.Contains(trimmed, "(") {
			block, end, err := extractBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("validate block: %w", err)
			}
			parseValidateBlock(block, cfg)
			i = end + 1
			continue
		}

		// Everything else is pipeline DSL
		pipelineLines = append(pipelineLines, lines[i])
		i++
	}

	cfg.PipelineDSL = strings.TrimSpace(strings.Join(pipelineLines, "\n"))
	return cfg, nil
}

// inBlock checks if we're currently inside a known block (lookahead)
func inBlock(lines []string, pos int, keywords ...string) bool {
	// Look backwards for an opening block
	for j := pos - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		for _, kw := range keywords {
			if strings.HasPrefix(t, kw) && strings.Contains(t, "(") {
				// Check if we haven't closed yet
				depth := 0
				for k := j; k <= pos; k++ {
					depth += strings.Count(lines[k], "(") - strings.Count(lines[k], ")")
				}
				if depth > 0 {
					return true
				}
			}
		}
	}
	return false
}

// extractBlock extracts content between ( and matching )
// Handles parens inside quoted strings and backtick strings
func extractBlock(lines []string, startLine int) (string, int, error) {
	combined := ""
	depth := 0
	started := false
	inDouble := false
	inSingle := false
	inBacktick := false
	prevChar := rune(0)

	for i := startLine; i < len(lines); i++ {
		line := lines[i]

		for _, c := range line {
			inQuote := inDouble || inSingle || inBacktick

			// Track quote state
			if !inQuote {
				if c == '"' {
					inDouble = true
				} else if c == '\'' {
					inSingle = true
				} else if c == '`' {
					inBacktick = true
				}
			} else {
				if c == '"' && inDouble && prevChar != '\\' {
					inDouble = false
				} else if c == '\'' && inSingle && prevChar != '\\' {
					inSingle = false
				} else if c == '`' && inBacktick {
					inBacktick = false
				}
			}

			// Only track parens outside of strings
			if !inQuote && !inDouble && !inSingle && !inBacktick {
				if c == '(' {
					if !started {
						started = true
						depth = 1
						prevChar = c
						continue
					}
					depth++
				} else if c == ')' && started {
					depth--
					if depth == 0 {
						return strings.TrimSpace(combined), i, nil
					}
				}
			}

			if started && depth > 0 {
				combined += string(c)
			}
			prevChar = c
		}
		if started && depth > 0 {
			combined += "\n"
		}
	}

	return "", len(lines), fmt.Errorf("unclosed block starting at line %d", startLine+1)
}

// parseTopLevelConfig handles config directives at the converge top level.
// Returns true if the line was consumed as config.
func parseTopLevelConfig(trimmed string, cfg *Config) bool {
	// sandbox "dir/"
	if strings.HasPrefix(trimmed, "sandbox ") {
		cfg.Sandbox = unquote(strings.TrimPrefix(trimmed, "sandbox "))
		return true
	}

	// reasoner claude
	// reasoner ollama "llama3:8b"
	if strings.HasPrefix(trimmed, "reasoner ") {
		parts := splitArgs(strings.TrimPrefix(trimmed, "reasoner "))
		if len(parts) >= 1 {
			cfg.Reasoner = parts[0]
		}
		if len(parts) >= 2 {
			cfg.ReasonerModel = unquote(parts[1])
		}
		return true
	}

	// history 3
	if strings.HasPrefix(trimmed, "history ") {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "history "))); err == nil {
			cfg.HistoryCount = n
		}
		return true
	}

	// mode patch | mode scaffold
	if strings.HasPrefix(trimmed, "mode ") {
		m := strings.TrimSpace(strings.TrimPrefix(trimmed, "mode "))
		if m == "patch" {
			cfg.PatchMode = true
		}
		return true
	}

	// branch "feature/xyz"
	if strings.HasPrefix(trimmed, "branch ") {
		cfg.Branch = unquote(strings.TrimPrefix(trimmed, "branch "))
		return true
	}

	// base "main"
	if strings.HasPrefix(trimmed, "base ") {
		cfg.Base = unquote(strings.TrimPrefix(trimmed, "base "))
		return true
	}

	// auto_commit true
	if strings.HasPrefix(trimmed, "auto_commit ") {
		cfg.AutoCommit = strings.TrimSpace(strings.TrimPrefix(trimmed, "auto_commit ")) == "true"
		return true
	}

	// auto_rebase true
	if strings.HasPrefix(trimmed, "auto_rebase ") {
		cfg.AutoRebase = strings.TrimSpace(strings.TrimPrefix(trimmed, "auto_rebase ")) == "true"
		return true
	}

	// commit_prefix "converge"
	if strings.HasPrefix(trimmed, "commit_prefix ") {
		cfg.CommitPrefix = unquote(strings.TrimPrefix(trimmed, "commit_prefix "))
		return true
	}

	return false
}

// parseContextBlock parses the body of context(...)
func parseContextBlock(block string, cfg *Config) {
	lines := strings.Split(block, "\n")
	var dslLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Try top-level config (backward compat — these can be in context too)
		if parseTopLevelConfig(trimmed, cfg) {
			continue
		}

		// Everything else is DSL to run as context
		dslLines = append(dslLines, line)
	}

	cfg.ContextDSL = strings.TrimSpace(strings.Join(dslLines, "\n"))
}

// parseIntentHeader parses "intent 5 60 propose" or "intent 5 60 auto"
func parseIntentHeader(header string, cfg *Config) {
	parts := splitArgs(header)
	// parts[0] = "intent"
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if p == "propose" || p == "auto" {
			cfg.Mode = p
			continue
		}
		if p == "(" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			if cfg.MaxRetries == 5 && n > 0 { // first number = retries
				cfg.MaxRetries = n
			} else { // second number = delay
				cfg.RetryDelay = n
			}
		}
	}
}

// parseIntentBlock parses the body of intent(...)
func parseIntentBlock(block string, cfg *Config) {
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		// Each line is a quoted intent string
		cfg.Intents = append(cfg.Intents, unquote(trimmed))
	}
}

// parseValidateBlock parses the body of validate(...)
func parseValidateBlock(block string, cfg *Config) {
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		// exec "command" — extract the command
		if strings.HasPrefix(trimmed, "exec ") {
			cmd := unquote(strings.TrimPrefix(trimmed, "exec "))
			cfg.ValidateCmds = append(cfg.ValidateCmds, cmd)
			continue
		}
		// Bare command string
		cfg.ValidateCmds = append(cfg.ValidateCmds, unquote(trimmed))
	}
}

// unquote strips surrounding quotes from a string
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitArgs splits a line into whitespace-separated tokens, respecting quotes
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
				current.WriteByte(c)
			} else {
				current.WriteByte(c)
			}
		} else {
			if c == '"' || c == '\'' {
				inQuote = true
				quoteChar = c
				current.WriteByte(c)
			} else if c == ' ' || c == '\t' {
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			} else {
				current.WriteByte(c)
			}
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
