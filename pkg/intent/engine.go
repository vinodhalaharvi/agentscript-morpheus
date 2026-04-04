package intent

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds parsed intent configuration
type Config struct {
	Sandbox       string
	Reasoner      string // "claude" or "ollama"
	ReasonerModel string // model name for ollama
	MaxRetries    int
	RetryDelay    int // seconds
	Mode          string // "propose" or "auto"
	ContextDSL    string // raw DSL for context steps
	Intents       []string
	ValidateCmds  []string // shell commands that must exit 0
	HistoryCount  int
	PipelineDSL   string // the main pipeline DSL (everything before context/intent/validate)
}

// HistoryEntry records a previous loop attempt
type HistoryEntry struct {
	Attempt  int
	Failures []string
	Proposal string
	Accepted bool
}

// ValidateResult holds one validation check result
type ValidateResult struct {
	Command  string
	Output   string
	ExitCode int
	Passed   bool
}

// ReasonerFunc is the function that calls the AI
type ReasonerFunc func(prompt string) (string, error)

// RunDSLFunc runs AgentScript DSL and returns the output
type RunDSLFunc func(dsl string) (string, error)

// Sandbox enforces file path boundaries
type Sandbox struct {
	Root string
}

// NewSandbox creates a sandbox rooted at the given directory
func NewSandbox(root string) (*Sandbox, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("sandbox: invalid path %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("sandbox: cannot create %q: %w", abs, err)
	}
	return &Sandbox{Root: abs}, nil
}

// IsSafe checks if a path resolves inside the sandbox
func (s *Sandbox) IsSafe(path string) bool {
	abs := s.resolve(path)
	return strings.HasPrefix(abs, s.Root)
}

func (s *Sandbox) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(s.Root, path))
}

// Exec runs a command inside the sandbox directory
func (s *Sandbox) Exec(command string) (string, int) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = s.Root
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(out), exitErr.ExitCode()
		}
		return string(out) + "\n" + err.Error(), 1
	}
	return string(out), 0
}

// ReadAll reads all files in the sandbox recursively
func (s *Sandbox) ReadAll() string {
	var sb strings.Builder
	filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(s.Root, path)
		if rel == "." {
			return nil
		}
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if info.IsDir() || info.Size() > 100*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", rel, string(data)))
		return nil
	})
	return sb.String()
}

// WriteFile writes a file inside the sandbox (with safety check)
func (s *Sandbox) WriteFile(path, content string) error {
	abs := s.resolve(path)
	if !strings.HasPrefix(abs, s.Root) {
		return fmt.Errorf("sandbox violation: %q escapes %q", path, s.Root)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0644)
}

// Engine runs the intent reconciliation loop
type Engine struct {
	config   Config
	sandbox  *Sandbox
	history  []HistoryEntry
	reasoner ReasonerFunc
	runDSL   RunDSLFunc
}

// NewEngine creates a new intent engine
func NewEngine(cfg Config, reasoner ReasonerFunc, runDSL RunDSLFunc) (*Engine, error) {
	sb, err := NewSandbox(cfg.Sandbox)
	if err != nil {
		return nil, err
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 5
	}
	if cfg.HistoryCount <= 0 {
		cfg.HistoryCount = 3
	}
	if cfg.Mode == "" {
		cfg.Mode = "propose"
	}
	return &Engine{
		config:   cfg,
		sandbox:  sb,
		history:  make([]HistoryEntry, 0),
		reasoner: reasoner,
		runDSL:   runDSL,
	}, nil
}

// Run executes the reconciliation loop
func (e *Engine) Run() error {
	// Run the pipeline DSL first (if any)
	if e.config.PipelineDSL != "" {
		fmt.Println("▶ Running pipeline...")
		if _, err := e.runDSL(e.config.PipelineDSL); err != nil {
			fmt.Printf("⚠️  Pipeline error: %v\n", err)
		}
	}

	for attempt := 1; attempt <= e.config.MaxRetries; attempt++ {
		fmt.Printf("\n🔄 ═══ Intent Loop — Attempt %d/%d ═══\n\n", attempt, e.config.MaxRetries)

		// 1. Collect context
		fmt.Println("📋 Collecting context...")
		contextOutput := e.collectContext()

		// 2. Run validations
		fmt.Println("\n✔️  Running validations...")
		results, allPassed := e.runValidations()
		for _, r := range results {
			if r.Passed {
				fmt.Printf("  ✅ %s\n", r.Command)
			} else {
				fmt.Printf("  ❌ %s\n", r.Command)
				lines := strings.Split(strings.TrimSpace(r.Output), "\n")
				for i, l := range lines {
					if i >= 5 {
						fmt.Printf("     ... (%d more lines)\n", len(lines)-5)
						break
					}
					fmt.Printf("     %s\n", l)
				}
			}
		}

		// 3. All passed — done!
		if allPassed {
			fmt.Println("\n✨ All validations passed! Intent satisfied.")
			return nil
		}

		// 4. Build prompt and ask AI
		prompt := e.buildPrompt(attempt, contextOutput, results)
		fmt.Println("\n🧠 Asking AI for a fix...")
		proposal, err := e.reasoner(prompt)
		if err != nil {
			fmt.Printf("❌ Reasoner error: %v\n", err)
			continue
		}

		// 5. Propose or auto-apply
		accepted := false
		if e.config.Mode == "auto" {
			fmt.Println("\n⚡ Auto mode — applying changes...")
			accepted = true
		} else {
			accepted = e.propose(attempt, proposal)
		}

		if accepted {
			if err := e.applyProposal(proposal); err != nil {
				fmt.Printf("❌ Apply error: %v\n", err)
			}
		}

		e.history = append(e.history, HistoryEntry{
			Attempt:  attempt,
			Failures: e.failureNames(results),
			Proposal: proposal,
			Accepted: accepted,
		})

		// 6. Wait before next loop
		if attempt < e.config.MaxRetries {
			fmt.Printf("\n⏳ Waiting %ds...\n", e.config.RetryDelay)
			time.Sleep(time.Duration(e.config.RetryDelay) * time.Second)
		}
	}

	fmt.Println("\n⛔ Max retries reached. Intent not fully satisfied.")
	return fmt.Errorf("intent not satisfied after %d attempts", e.config.MaxRetries)
}

// collectContext gathers current state
func (e *Engine) collectContext() string {
	var sb strings.Builder

	// Always include file listing
	sb.WriteString("## FILES IN SANDBOX\n")
	treeOut, _ := e.sandbox.Exec("find . -type f | head -100")
	sb.WriteString(treeOut)
	sb.WriteString("\n")

	// Read all file contents
	sb.WriteString("## FILE CONTENTS\n")
	sb.WriteString(e.sandbox.ReadAll())

	// Run context DSL commands if any
	if e.config.ContextDSL != "" {
		sb.WriteString("## CONTEXT COMMAND OUTPUT\n")
		out, err := e.runDSL(e.config.ContextDSL)
		if err != nil {
			sb.WriteString(fmt.Sprintf("(context DSL error: %v)\n", err))
		} else {
			sb.WriteString(out)
		}
		sb.WriteString("\n")
	}

	// Run validate commands and capture output (for context)
	sb.WriteString("## BUILD/TEST OUTPUT\n")
	for _, cmd := range e.config.ValidateCmds {
		out, exitCode := e.sandbox.Exec(cmd)
		sb.WriteString(fmt.Sprintf("--- %s (exit %d) ---\n%s\n", cmd, exitCode, out))
	}

	return sb.String()
}

// runValidations checks all validate commands
func (e *Engine) runValidations() ([]ValidateResult, bool) {
	var results []ValidateResult
	allPassed := true
	for _, cmd := range e.config.ValidateCmds {
		out, exitCode := e.sandbox.Exec(cmd)
		passed := exitCode == 0
		if !passed {
			allPassed = false
		}
		results = append(results, ValidateResult{
			Command:  cmd,
			Output:   out,
			ExitCode: exitCode,
			Passed:   passed,
		})
	}
	return results, allPassed
}

// buildPrompt constructs the AI prompt
func (e *Engine) buildPrompt(attempt int, contextOutput string, results []ValidateResult) string {
	var sb strings.Builder

	sb.WriteString("You are an expert Go engineer. You are building a project inside a sandboxed directory.\n\n")

	sb.WriteString("## INTENT (desired end state)\n")
	for _, intent := range e.config.Intents {
		sb.WriteString(fmt.Sprintf("- %s\n", intent))
	}

	sb.WriteString("\n## VALIDATION RESULTS\n")
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", status, r.Command))
		if !r.Passed && r.Output != "" {
			sb.WriteString(fmt.Sprintf("```\n%s```\n", r.Output))
		}
	}

	sb.WriteString("\n## CURRENT PROJECT STATE\n")
	sb.WriteString(contextOutput)

	// History
	if len(e.history) > 0 {
		start := len(e.history) - e.config.HistoryCount
		if start < 0 {
			start = 0
		}
		sb.WriteString("\n## PREVIOUS ATTEMPTS (do NOT repeat failed approaches)\n")
		for _, h := range e.history[start:] {
			sb.WriteString(fmt.Sprintf("Attempt %d: failures=%v accepted=%v\n", h.Attempt, h.Failures, h.Accepted))
		}
	}

	sb.WriteString("\n## RESPONSE FORMAT\n")
	sb.WriteString("Respond with ONLY a JSON object with two keys: \"files\" and \"commands\".\n")
	sb.WriteString("\"files\" is an object where keys are file paths and values are COMPLETE file contents.\n")
	sb.WriteString("\"commands\" is an array of shell commands to run AFTER files are written (e.g. go mod tidy, buf generate).\n")
	sb.WriteString("```json\n{\n  \"files\": {\n    \"go.mod\": \"module myapp\\n...\",\n    \"cmd/main.go\": \"package main\\n...\"\n  },\n  \"commands\": [\n    \"go mod tidy\",\n    \"buf generate\"\n  ]\n}\n```\n")
	sb.WriteString("Only include files that need to be created or modified. Do NOT include go.sum — use 'go mod tidy' in commands instead.\n")
	sb.WriteString("Commands run inside the sandbox directory. Use commands for things that cannot be expressed as file contents.\n")
	sb.WriteString(fmt.Sprintf("This is attempt %d of %d. Fix ALL validation failures.\n", attempt, e.config.MaxRetries))

	return sb.String()
}

// propose shows the plan and waits for human approval
func (e *Engine) propose(attempt int, proposal string) bool {
	files, commands := ParseProposal(proposal)

	fmt.Printf("\n┌──────────────────────────────────────────────────┐\n")
	fmt.Printf("│  PROPOSED FIX (attempt %d/%d)                       \n", attempt, e.config.MaxRetries)
	fmt.Printf("│                                                  │\n")
	if len(files) > 0 {
		for path, content := range files {
			lines := strings.Count(content, "\n") + 1
			fmt.Printf("│  📄 %-35s (%d lines)\n", path, lines)
		}
	}
	if len(commands) > 0 {
		fmt.Printf("│                                                  │\n")
		fmt.Printf("│  🔧 Commands:                                    │\n")
		for i, cmd := range commands {
			fmt.Printf("│     %d. %-42s\n", i+1, cmd)
		}
	}
	if len(files) == 0 && len(commands) == 0 {
		lines := strings.Split(proposal, "\n")
		for i, l := range lines {
			if i >= 8 {
				fmt.Printf("│  ... (%d more lines)\n", len(lines)-8)
				break
			}
			if len(l) > 55 {
				l = l[:55] + "..."
			}
			fmt.Printf("│  %s\n", l)
		}
	}
	fmt.Printf("│                                                  │\n")
	fmt.Printf("│  [y]es  [n]o  [v]iew  [s]kip                    │\n")
	fmt.Printf("└──────────────────────────────────────────────────┘\n")
	fmt.Print("\n> ")

	reader := bufio.NewReader(os.Stdin)
	for {
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		switch input {
		case "y", "yes":
			return true
		case "n", "no":
			fmt.Println("❌ Aborted.")
			os.Exit(0)
			return false
		case "v", "view":
			fmt.Println("\n--- Full Proposal ---")
			fmt.Println(proposal)
			fmt.Println("--- End ---\n")
			fmt.Print("[y/n/s] > ")
		case "s", "skip":
			fmt.Println("⏭  Skipped.")
			return false
		default:
			fmt.Print("[y/n/v/s] > ")
		}
	}
}

// applyProposal writes files and runs commands from the AI response
func (e *Engine) applyProposal(proposal string) error {
	files, commands := ParseProposal(proposal)

	// Write files
	if len(files) > 0 {
		for path, content := range files {
			if !e.sandbox.IsSafe(path) {
				fmt.Printf("🚫 Sandbox violation: %s — skipped\n", path)
				continue
			}
			if err := e.sandbox.WriteFile(path, content); err != nil {
				fmt.Printf("❌ Write failed %s: %v\n", path, err)
				continue
			}
			lines := strings.Count(content, "\n") + 1
			fmt.Printf("  ✏️  %s (%d lines)\n", path, lines)
		}
	}

	// Run commands
	if len(commands) > 0 {
		fmt.Println()
		for _, cmd := range commands {
			fmt.Printf("  🔧 Running: %s\n", cmd)
			out, exitCode := e.sandbox.Exec(cmd)
			if exitCode != 0 {
				fmt.Printf("     ⚠️  exit %d: %s\n", exitCode, strings.TrimSpace(firstLines(out, 3)))
			} else {
				fmt.Printf("     ✅ done\n")
			}
		}
	}

	if len(files) == 0 && len(commands) == 0 {
		fmt.Println("⚠️  No files or commands found in proposal.")
	}

	return nil
}

// firstLines returns the first n lines of a string
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func (e *Engine) failureNames(results []ValidateResult) []string {
	var out []string
	for _, r := range results {
		if !r.Passed {
			out = append(out, r.Command)
		}
	}
	return out
}

// ParseProposal extracts files and commands from an AI response.
// Supports two formats:
//   1. New: {"files": {path: content}, "commands": ["cmd1", "cmd2"]}
//   2. Legacy: {path: content} (no commands)
func ParseProposal(response string) (map[string]string, []string) {
	response = strings.TrimSpace(response)

	// Strip markdown fences
	if idx := strings.Index(response, "```json"); idx >= 0 {
		response = response[idx+7:]
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
	} else if idx := strings.Index(response, "```"); idx >= 0 {
		response = response[idx+3:]
		if nl := strings.Index(response, "\n"); nl >= 0 {
			response = response[nl+1:]
		}
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
	}

	response = strings.TrimSpace(response)
	if idx := strings.Index(response, "{"); idx >= 0 {
		response = response[idx:]
	}
	if idx := strings.LastIndex(response, "}"); idx >= 0 {
		response = response[:idx+1]
	}

	// Try to detect new format: look for "files" and "commands" keys
	if strings.Contains(response, `"files"`) {
		files, commands := parseNewFormat(response)
		if len(files) > 0 || len(commands) > 0 {
			return files, commands
		}
	}

	// Fall back to legacy format: {path: content}
	files := ParseFiles(response)
	return files, nil
}

// parseNewFormat extracts from {"files": {...}, "commands": [...]}
func parseNewFormat(response string) (map[string]string, []string) {
	files := make(map[string]string)
	var commands []string

	// Extract the "files" object
	filesIdx := strings.Index(response, `"files"`)
	if filesIdx >= 0 {
		// Find the opening { after "files":
		rest := response[filesIdx+7:]
		colonIdx := strings.Index(rest, ":")
		if colonIdx >= 0 {
			rest = rest[colonIdx+1:]
			// Find matching { }
			braceStart := strings.Index(rest, "{")
			if braceStart >= 0 {
				depth := 0
				inStr := false
				esc := false
				end := -1
				for i := braceStart; i < len(rest); i++ {
					c := rest[i]
					if esc {
						esc = false
						continue
					}
					if c == '\\' && inStr {
						esc = true
						continue
					}
					if c == '"' {
						inStr = !inStr
					}
					if !inStr {
						if c == '{' {
							depth++
						} else if c == '}' {
							depth--
							if depth == 0 {
								end = i
								break
							}
						}
					}
				}
				if end >= 0 {
					filesJSON := rest[braceStart : end+1]
					files = ParseFiles(filesJSON)
				}
			}
		}
	}

	// Extract the "commands" array
	cmdsIdx := strings.Index(response, `"commands"`)
	if cmdsIdx >= 0 {
		rest := response[cmdsIdx+10:]
		colonIdx := strings.Index(rest, ":")
		if colonIdx >= 0 {
			rest = rest[colonIdx+1:]
			bracketStart := strings.Index(rest, "[")
			if bracketStart >= 0 {
				bracketEnd := strings.Index(rest[bracketStart:], "]")
				if bracketEnd >= 0 {
					arrStr := rest[bracketStart : bracketStart+bracketEnd+1]
					// Simple extraction of quoted strings from array
					inQ := false
					esc := false
					var current strings.Builder
					for i := 1; i < len(arrStr)-1; i++ {
						c := arrStr[i]
						if esc {
							switch c {
							case 'n':
								current.WriteByte('\n')
							case 't':
								current.WriteByte('\t')
							case '"':
								current.WriteByte('"')
							case '\\':
								current.WriteByte('\\')
							default:
								current.WriteByte('\\')
								current.WriteByte(c)
							}
							esc = false
							continue
						}
						if c == '\\' && inQ {
							esc = true
							continue
						}
						if c == '"' {
							if !inQ {
								inQ = true
							} else {
								inQ = false
								cmd := strings.TrimSpace(current.String())
								if cmd != "" {
									commands = append(commands, cmd)
								}
								current.Reset()
							}
							continue
						}
						if inQ {
							current.WriteByte(c)
						}
					}
				}
			}
		}
	}

	return files, commands
}

// ParseFiles extracts {path: content} from an AI response (legacy format)
func ParseFiles(response string) map[string]string {
	response = strings.TrimSpace(response)

	// Strip markdown fences
	if idx := strings.Index(response, "```json"); idx >= 0 {
		response = response[idx+7:]
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
	} else if idx := strings.Index(response, "```"); idx >= 0 {
		response = response[idx+3:]
		if nl := strings.Index(response, "\n"); nl >= 0 {
			response = response[nl+1:]
		}
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
	}

	// Find the JSON object
	response = strings.TrimSpace(response)
	if idx := strings.Index(response, "{"); idx >= 0 {
		response = response[idx:]
	}
	if idx := strings.LastIndex(response, "}"); idx >= 0 {
		response = response[:idx+1]
	}

	// Parse JSON with escaped newlines using state machine
	result := make(map[string]string)
	if len(response) < 2 || response[0] != '{' {
		return result
	}

	inner := response[1 : len(response)-1]
	var key, value strings.Builder
	inString := false
	escaped := false
	parsingKey := true
	gotKey := false

	for i := 0; i < len(inner); i++ {
		c := inner[i]

		if escaped {
			switch c {
			case 'n':
				if parsingKey {
					key.WriteByte('\n')
				} else {
					value.WriteByte('\n')
				}
			case 't':
				if parsingKey {
					key.WriteByte('\t')
				} else {
					value.WriteByte('\t')
				}
			case '\\':
				if parsingKey {
					key.WriteByte('\\')
				} else {
					value.WriteByte('\\')
				}
			case '"':
				if parsingKey {
					key.WriteByte('"')
				} else {
					value.WriteByte('"')
				}
			case '/':
				if parsingKey {
					key.WriteByte('/')
				} else {
					value.WriteByte('/')
				}
			default:
				if parsingKey {
					key.WriteByte('\\')
					key.WriteByte(c)
				} else {
					value.WriteByte('\\')
					value.WriteByte(c)
				}
			}
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			if !inString {
				inString = true
			} else {
				inString = false
				if parsingKey {
					gotKey = true
				} else {
					result[key.String()] = value.String()
					key.Reset()
					value.Reset()
					parsingKey = true
					gotKey = false
				}
			}
			continue
		}

		if !inString {
			if c == ':' && gotKey {
				parsingKey = false
				continue
			}
			continue
		}

		if parsingKey {
			key.WriteByte(c)
		} else {
			value.WriteByte(c)
		}
	}

	return result
}
