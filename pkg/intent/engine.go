package intent

import (
	"bufio"
	"encoding/json"
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
	UseSession    bool   // true = multi-turn conversation, false = single-shot
	MaxRetries    int
	RetryDelay    int // seconds
	Mode          string // "propose" or "auto"
	ContextDSL    string // raw DSL for context steps
	Intents       []string
	ValidateCmds  []string // shell commands that must exit 0
	HistoryCount  int
	PipelineDSL   string // the main pipeline DSL (everything before context/intent/validate)

	// Git patch mode
	PatchMode    bool   // true = unified diffs, false = full files (scaffold)
	Branch       string // git branch to create/checkout
	Base         string // base branch for rebase (e.g. "main")
	AutoCommit   bool   // commit after each accepted proposal
	AutoRebase   bool   // rebase on base before each loop
	CommitPrefix string // prefix for commit messages (default "converge")
}

// HistoryEntry records a previous loop attempt.
//
// Note: we intentionally do NOT store the full AI proposal text here.
// Proposals can be 10-50KB of JSON with file contents, and they are
// never read back — only Failures + ApplyErrors are ever referenced
// in buildPrompt. In session mode Claude already remembers prior
// proposals via the message history.
type HistoryEntry struct {
	Attempt     int
	Failures    []string
	Accepted    bool
	ApplyErrors []string
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

// TokenReportFunc is called after each AI call with usage info
type TokenReportFunc func()

// CompactFunc optionally trims older context from the underlying reasoner
// (e.g. a multi-turn LLM session). Called after each accepted proposal has
// been applied and committed. The engine is agnostic to the trimming
// strategy — the caller decides what to keep.
//
// Why this exists: in session mode the reasoner resends the whole message
// history on every call. Old assistant responses (huge JSON proposals) are
// dead weight once the engine has applied them to disk — compacting them
// holds per-call input-token cost roughly flat across loops instead of
// letting it grow linearly.
type CompactFunc func()

// Engine runs the intent reconciliation loop
type Engine struct {
	config         Config
	sandbox        *Sandbox
	history        []HistoryEntry
	reasoner       ReasonerFunc
	runDSL         RunDSLFunc
	tokenReport    TokenReportFunc
	compact        CompactFunc
	diagnosticCmds []string // accumulated diagnostic commands from AI
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

// SetTokenReporter sets a function to call after each AI call for token reporting
func (e *Engine) SetTokenReporter(fn TokenReportFunc) {
	e.tokenReport = fn
}

// SetCompactor sets a function to call after each accepted proposal is
// applied. Typically used in session mode to trim older assistant responses
// from the LLM message history. Optional — no-op if unset.
func (e *Engine) SetCompactor(fn CompactFunc) {
	e.compact = fn
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

	// Git setup — create/checkout branch if patch mode
	if e.config.PatchMode && e.config.Branch != "" {
		e.gitSetup()
	}

	// Auto-init git repo if auto_commit is enabled
	if e.config.AutoCommit {
		gitDir := filepath.Join(e.sandbox.Root, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			fmt.Println("📦 Initializing git repo for auto-commit...")
			e.sandbox.Exec("git init")
			e.sandbox.Exec("git add -A")
			e.sandbox.Exec("git commit -m 'initial state'")
		}
	}

	if e.config.CommitPrefix == "" {
		e.config.CommitPrefix = "converge"
	}

	var lastResults []ValidateResult

	for attempt := 1; attempt <= e.config.MaxRetries; attempt++ {
		fmt.Printf("\n🔄 ═══ Intent Loop — Attempt %d/%d ═══\n\n", attempt, e.config.MaxRetries)

		// Git rebase if enabled
		if e.config.AutoRebase && e.config.Base != "" {
			e.gitRebase()
		}

		// 1. Collect context + adaptive diagnostics from last failure.
		//
		// In session mode (loop 2+), buildPrompt takes the follow-up
		// branch and never uses contextOutput — Claude remembers the
		// prior context from the conversation history. Running all the
		// context DSL commands + validate commands + diagnostic commands
		// just to discard the output is a significant waste of time and
		// compute, especially when context DSL contains slow things
		// like `go test` or `tree` over large trees.
		//
		// We still always run step 2 (validations) below because those
		// feed buildPrompt on BOTH branches.
		isFollowUp := e.config.UseSession && len(e.history) > 0
		var contextOutput string
		if !isFollowUp {
			fmt.Println("📋 Collecting context...")
			contextOutput = e.collectContext()
			if len(lastResults) > 0 {
				diag := e.collectDiagnostics(lastResults)
				if diag != "" {
					contextOutput += "\n" + diag
				}
			}
		}

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
		if e.tokenReport != nil {
			e.tokenReport()
		}
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

		var applyErrors []string
		if accepted {
			applyErrors = e.applyProposal(proposal)

			// Git commit if enabled
			if e.config.AutoCommit {
				e.gitCommit(attempt)
			}

			// Compact older reasoner context (session mode only). The proposal
			// we just applied is now reflected on disk / in git — re-uploading
			// its full text on the next loop would be wasted tokens. In
			// single-shot or Ollama modes this is a no-op (compact stays nil).
			if e.compact != nil {
				e.compact()
			}
		}

		e.history = append(e.history, HistoryEntry{
			Attempt:     attempt,
			Failures:    e.failureNames(results),
			Accepted:    accepted,
			ApplyErrors: applyErrors,
		})

		// Store results for adaptive diagnostics on next loop
		lastResults = results

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

	// Always include file listing (not contents — too expensive)
	sb.WriteString("## FILES IN SANDBOX\n")
	treeOut, _ := e.sandbox.Exec("find . -type f -not -path './.git/*' -not -path './gen/*' | sort | head -100")
	sb.WriteString(treeOut)
	sb.WriteString("\n")

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

	// Run accumulated diagnostic commands from previous AI analysis
	if len(e.diagnosticCmds) > 0 {
		sb.WriteString("## DIAGNOSTIC CONTEXT (AI-discovered)\n")
		for _, cmd := range e.diagnosticCmds {
			out, _ := e.sandbox.Exec(cmd)
			sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n", cmd, out))
		}
	}

	return sb.String()
}

// collectDiagnostics uses the AI to analyze validation failures and
// determine what additional context is needed. The AI returns shell
// commands to run, and the engine adds them to its accumulated list.
// These commands persist across loops — the context grows smarter.
func (e *Engine) collectDiagnostics(results []ValidateResult) string {
	var sb strings.Builder
	sb.WriteString("You are debugging a project. These validations failed:\n\n")
	for _, r := range results {
		if !r.Passed {
			sb.WriteString(fmt.Sprintf("[FAIL] %s\n```\n%s\n```\n", r.Command, r.Output))
		}
	}

	// Tell Claude what diagnostics we already have
	if len(e.diagnosticCmds) > 0 {
		sb.WriteString("\nI am already running these diagnostic commands:\n")
		for _, cmd := range e.diagnosticCmds {
			sb.WriteString(fmt.Sprintf("  - %s\n", cmd))
		}
		sb.WriteString("\nSuggest additional commands NOT already in the list above.\n")
	}

	sb.WriteString("\nWhat shell commands should I run to gather diagnostic context that will help fix these errors?\n")
	sb.WriteString("Respond with ONLY a JSON array of shell commands. No explanation. Example:\n")
	sb.WriteString("[\"cat go.mod\", \"find gen -type f\", \"head -30 cmd/server/main.go\"]\n")
	sb.WriteString("Maximum 5 commands. Only read commands (cat, find, ls, head, grep). No modifications.\n")
	sb.WriteString("Return empty array [] if no additional diagnostics needed.\n")

	fmt.Println("🔍 Gathering adaptive diagnostics...")
	response, err := e.reasoner(sb.String())
	if e.tokenReport != nil {
		e.tokenReport()
	}
	if err != nil {
		return ""
	}

	commands := parseDiagnosticCommands(response)
	if len(commands) == 0 {
		return ""
	}

	// Filter safe commands and deduplicate
	existing := make(map[string]bool)
	for _, cmd := range e.diagnosticCmds {
		existing[cmd] = true
	}

	var newCmds []string
	for _, cmd := range commands {
		if !isSafeReadCommand(cmd) {
			fmt.Printf("   ⚠️  Skipped unsafe: %s\n", cmd)
			continue
		}
		if existing[cmd] {
			continue
		}
		newCmds = append(newCmds, cmd)
	}

	if len(newCmds) == 0 {
		return ""
	}

	// Show proposed diagnostic commands and ask for approval
	fmt.Printf("\n┌──────────────────────────────────────────────────┐\n")
	fmt.Printf("│  🔍 AI wants to run diagnostic commands:          │\n")
	fmt.Printf("│                                                  │\n")
	for i, cmd := range newCmds {
		fmt.Printf("│     %d. %-42s\n", i+1, cmd)
	}
	fmt.Printf("│                                                  │\n")
	fmt.Printf("│  [y]es  [n]o                                     │\n")
	fmt.Printf("└──────────────────────────────────────────────────┘\n")
	fmt.Print("\n> ")

	reader := bufio.NewReader(os.Stdin)
	for {
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "" {
			continue
		}
		if input == "y" || input == "yes" {
			break
		}
		if input == "n" || input == "no" {
			fmt.Println("⏭  Skipped diagnostics.")
			return ""
		}
		fmt.Print("[y/n] > ")
	}

	// Approved — add to accumulated list and run
	for _, cmd := range newCmds {
		e.diagnosticCmds = append(e.diagnosticCmds, cmd)
		fmt.Printf("   ➕ %s\n", cmd)
	}
	fmt.Printf("   📋 Total diagnostic commands: %d\n", len(e.diagnosticCmds))

	// Run new commands for immediate use
	var diag strings.Builder
	diag.WriteString("## NEW DIAGNOSTIC CONTEXT\n")
	for _, cmd := range newCmds {
		out, _ := e.sandbox.Exec(cmd)
		diag.WriteString(fmt.Sprintf("--- %s ---\n%s\n", cmd, out))
	}

	return diag.String()
}

// parseDiagnosticCommands extracts shell commands from AI response
func parseDiagnosticCommands(response string) []string {
	response = strings.TrimSpace(response)

	if idx := strings.Index(response, "```"); idx >= 0 {
		response = response[idx+3:]
		if strings.HasPrefix(response, "json") {
			response = response[4:]
		}
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
	}

	response = strings.TrimSpace(response)
	if idx := strings.Index(response, "["); idx >= 0 {
		response = response[idx:]
	}
	if idx := strings.LastIndex(response, "]"); idx >= 0 {
		response = response[:idx+1]
	}

	var commands []string
	if err := json.Unmarshal([]byte(response), &commands); err != nil {
		return nil
	}

	if len(commands) > 5 {
		commands = commands[:5]
	}
	return commands
}

// isSafeReadCommand checks that a command only reads, never modifies
func isSafeReadCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}

	safe := map[string]bool{
		"cat": true, "head": true, "tail": true, "find": true,
		"ls": true, "tree": true, "grep": true, "wc": true,
		"file": true, "stat": true, "du": true, "diff": true,
		"pureast": true, "gopls": true,
	}

	base := parts[0]
	if safe[base] {
		return true
	}

	if base == "go" && len(parts) >= 2 {
		safeGo := map[string]bool{
			"list": true, "doc": true, "env": true, "version": true,
		}
		return safeGo[parts[1]]
	}

	return false
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

	isFollowUp := e.config.UseSession && len(e.history) > 0

	if isFollowUp {
		// Session mode, loop 2+: Claude remembers everything, just send what changed
		sb.WriteString(fmt.Sprintf("## ATTEMPT %d/%d — FOLLOW UP\n\n", attempt, e.config.MaxRetries))

		// Only send validation results and errors
		sb.WriteString("## VALIDATION RESULTS AFTER YOUR LAST FIX\n")
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

		// Send command errors from last apply if any
		if len(e.history) > 0 {
			last := e.history[len(e.history)-1]
			if len(last.ApplyErrors) > 0 {
				sb.WriteString("\n## COMMAND ERRORS FROM YOUR LAST PROPOSAL\n")
				for _, err := range last.ApplyErrors {
					sb.WriteString(fmt.Sprintf("- %s\n", err))
				}
			}
		}

		// Send git diff if auto-commit is on
		if e.config.AutoCommit {
			sb.WriteString("\n## GIT DIFF (what changed from your last fix)\n")
			diff, _ := e.sandbox.Exec("git diff HEAD~1 2>/dev/null")
			if strings.TrimSpace(diff) != "" {
				sb.WriteString(diff)
			}
		}

		sb.WriteString("\nFix the remaining failures. Respond with ONLY JSON, no prose:\n")
		sb.WriteString("```json\n")
		if e.config.PatchMode {
			sb.WriteString("{\"patches\": [{\"file\": \"path\", \"diff\": \"--- a/...\\n+++ b/...\"}], \"files\": {}, \"commands\": []}\n")
		} else {
			sb.WriteString("{\"files\": {\"path/to/file.go\": \"content...\"}, \"commands\": [\"go mod tidy\"]}\n")
		}
		sb.WriteString("```\n")
		return sb.String()
	}

	// First attempt (or single-shot mode): send everything
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

	// History (only for single-shot mode — session mode doesn't need it)
	if !e.config.UseSession && len(e.history) > 0 {
		start := len(e.history) - e.config.HistoryCount
		if start < 0 {
			start = 0
		}
		sb.WriteString("\n## PREVIOUS ATTEMPTS (do NOT repeat failed approaches)\n")
		for _, h := range e.history[start:] {
			sb.WriteString(fmt.Sprintf("Attempt %d: validation_failures=%v accepted=%v\n", h.Attempt, h.Failures, h.Accepted))
			if len(h.ApplyErrors) > 0 {
				sb.WriteString("  COMMAND ERRORS (these commands FAILED — do NOT repeat the same approach):\n")
				for _, e := range h.ApplyErrors {
					sb.WriteString(fmt.Sprintf("  - %s\n", e))
				}
			}
		}
	}

	sb.WriteString("\n## RESPONSE FORMAT — CRITICAL\n")
	sb.WriteString("You MUST respond with ONLY a JSON object. No explanation. No analysis. No markdown outside the JSON. JUST JSON.\n\n")

	if e.config.PatchMode && len(e.history) > 0 {
		// Patch mode (loop 2+): return unified diffs for existing files, full content for new files
		sb.WriteString("Return a JSON object with three keys: \"patches\", \"files\", and \"commands\".\n")
		sb.WriteString("\"patches\": array of objects with \"file\" (path) and \"diff\" (unified diff format).\n")
		sb.WriteString("\"files\": object for NEW files only (key=path, value=full content). Do NOT use this for existing files.\n")
		sb.WriteString("\"commands\": array of shell commands to run after changes are applied.\n")
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"patches\": [\n")
		sb.WriteString("    {\n")
		sb.WriteString("      \"file\": \"internal/server/user.go\",\n")
		sb.WriteString("      \"diff\": \"--- a/internal/server/user.go\\n+++ b/internal/server/user.go\\n@@ -10,3 +10,5 @@\\n func foo() {\\n-    old line\\n+    new line\\n+    added line\\n }\\n\"\n")
		sb.WriteString("    }\n")
		sb.WriteString("  ],\n")
		sb.WriteString("  \"files\": {\n")
		sb.WriteString("    \"internal/server/new_file.go\": \"package server\\n...\"\n")
		sb.WriteString("  },\n")
		sb.WriteString("  \"commands\": [\"go mod tidy\"]\n")
		sb.WriteString("}\n```\n")
		sb.WriteString("Use \"patches\" for modifying existing files. Use \"files\" only for brand new files.\n")
		sb.WriteString("Diffs must be valid unified diff format that git apply can process.\n")
	} else {
		// Scaffold mode or first attempt: return full file contents
		sb.WriteString("Return a JSON object with two keys: \"files\" and \"commands\".\n")
		sb.WriteString("\"files\": object where keys are file paths, values are COMPLETE file contents.\n")
		sb.WriteString("\"commands\": array of shell commands to run AFTER files are written.\n")
		sb.WriteString("If you have nothing to change, respond with: {\"files\": {}, \"commands\": []}\n")
		sb.WriteString("```json\n{\n  \"files\": {\n    \"path/to/file.go\": \"package main\\n...\"\n  },\n  \"commands\": [\"go mod tidy\"]\n}\n```\n")
	}

	sb.WriteString("IMPORTANT: Do NOT include any text before or after the JSON. No analysis. ONLY JSON.\n")
	sb.WriteString("Do NOT include go.sum in files — use 'go mod tidy' in commands.\n")

	if e.config.PatchMode {
		sb.WriteString("\n## GIT CONTEXT\n")
		sb.WriteString("This project is under git version control.\n")
		if e.config.Branch != "" {
			sb.WriteString(fmt.Sprintf("Working branch: %s\n", e.config.Branch))
		}
		if e.config.Base != "" {
			sb.WriteString(fmt.Sprintf("Base branch: %s\n", e.config.Base))
		}
		sb.WriteString("Each accepted proposal will be committed automatically. Keep changes focused and atomic.\n")
		sb.WriteString("Avoid modifying files unrelated to the current validation failure.\n")
	}

	sb.WriteString(fmt.Sprintf("\nThis is attempt %d of %d. Fix ALL validation failures.\n", attempt, e.config.MaxRetries))

	return sb.String()
}

// propose shows the plan and waits for human approval
func (e *Engine) propose(attempt int, proposal string) bool {
	prop := ParseProposalFull(proposal)

	fmt.Printf("\n┌──────────────────────────────────────────────────┐\n")
	fmt.Printf("│  PROPOSED FIX (attempt %d/%d)                       \n", attempt, e.config.MaxRetries)
	if e.config.Branch != "" {
		fmt.Printf("│  branch: %-40s\n", e.config.Branch)
	}
	fmt.Printf("│                                                  │\n")
	if len(prop.Patches) > 0 {
		for _, p := range prop.Patches {
			adds := strings.Count(p.Diff, "\n+")
			dels := strings.Count(p.Diff, "\n-")
			fmt.Printf("│  📝 %-35s (+%d, -%d)\n", p.File, adds, dels)
		}
	}
	if len(prop.Files) > 0 {
		for path, content := range prop.Files {
			lines := strings.Count(content, "\n") + 1
			fmt.Printf("│  📄 %-35s (%d lines)\n", path, lines)
		}
	}
	if len(prop.Commands) > 0 {
		fmt.Printf("│                                                  │\n")
		fmt.Printf("│  🔧 Commands:                                    │\n")
		for i, cmd := range prop.Commands {
			fmt.Printf("│     %d. %-42s\n", i+1, cmd)
		}
	}
	if e.config.AutoCommit {
		fmt.Printf("│                                                  │\n")
		fmt.Printf("│  📦 Will auto-commit after apply                 │\n")
	}
	if len(prop.Files) == 0 && len(prop.Commands) == 0 && len(prop.Patches) == 0 {
		fmt.Printf("│                                                  │\n")
		fmt.Printf("│  ⚠️  Could not parse JSON from AI response        │\n")
		fmt.Printf("│                                                  │\n")
		fmt.Printf("└──────────────────────────────────────────────────┘\n")
		// Show what Claude actually returned
		fmt.Println("\n--- AI Response (raw) ---")
		lines := strings.Split(proposal, "\n")
		for i, l := range lines {
			if i >= 20 {
				fmt.Printf("... (%d more lines, use [v]iew to see all)\n", len(lines)-20)
				break
			}
			fmt.Println(l)
		}
		fmt.Println("--- End ---")
		fmt.Print("\n[v]iew full | [s]kip | [n] abort > ")

		reader := bufio.NewReader(os.Stdin)
		for {
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))
			if input == "" {
				continue
			}
			switch input {
			case "v", "view":
				fmt.Println("\n--- Full Response ---")
				fmt.Println(proposal)
				fmt.Println("--- End ---")
				fmt.Print("\n[s]kip | [n] abort > ")
			case "s", "skip":
				return false
			case "n", "no":
				fmt.Println("❌ Aborted.")
				os.Exit(0)
				return false
			default:
				fmt.Print("[v/s/n] > ")
			}
		}
	}
	fmt.Printf("│                                                  │\n")
	fmt.Printf("│  [y]es  [n]o  [v]iew  [d]iff  [s]kip            │\n")
	fmt.Printf("└──────────────────────────────────────────────────┘\n")
	fmt.Print("\n> ")

	reader := bufio.NewReader(os.Stdin)
	for {
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "" {
			continue
		}
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
			fmt.Print("[y/n/d/s] > ")
		case "d", "diff":
			// Show patches
			for _, p := range prop.Patches {
				fmt.Printf("\n--- %s (patch) ---\n", p.File)
				fmt.Println(p.Diff)
			}
			// Show new files
			for path, content := range prop.Files {
				fmt.Printf("\n--- %s (new file) ---\n", path)
				lines := strings.Split(content, "\n")
				for i, l := range lines {
					if i >= 30 {
						fmt.Printf("... (%d more lines)\n", len(lines)-30)
						break
					}
					fmt.Printf("+ %s\n", l)
				}
			}
			fmt.Println()
			fmt.Print("[y/n/s] > ")
		case "s", "skip":
			fmt.Println("⏭  Skipped.")
			return false
		default:
			fmt.Print("[y/n/v/d/s] > ")
		}
	}
}

// applyProposal writes files, applies patches, and runs commands
func (e *Engine) applyProposal(proposal string) []string {
	prop := ParseProposalFull(proposal)
	var errors []string

	// Apply patches via git apply
	if len(prop.Patches) > 0 {
		for _, p := range prop.Patches {
			if !e.sandbox.IsSafe(p.File) {
				fmt.Printf("🚫 Sandbox violation: %s — skipped\n", p.File)
				continue
			}
			// Write patch to temp file and apply
			patchPath := filepath.Join(e.sandbox.Root, ".converge-patch.tmp")
			if err := os.WriteFile(patchPath, []byte(p.Diff), 0644); err != nil {
				errMsg := fmt.Sprintf("failed to write patch for %s: %v", p.File, err)
				fmt.Printf("❌ %s\n", errMsg)
				errors = append(errors, errMsg)
				continue
			}
			out, exitCode := e.sandbox.Exec("git apply .converge-patch.tmp 2>&1")
			os.Remove(patchPath)
			if exitCode != 0 {
				// Fallback: try with --3way
				os.WriteFile(patchPath, []byte(p.Diff), 0644)
				out, exitCode = e.sandbox.Exec("git apply --3way .converge-patch.tmp 2>&1")
				os.Remove(patchPath)
			}
			if exitCode != 0 {
				errMsg := fmt.Sprintf("patch failed for %s: %s", p.File, strings.TrimSpace(out))
				fmt.Printf("  ❌ patch %s: %s\n", p.File, strings.TrimSpace(firstLines(out, 2)))
				errors = append(errors, errMsg)
			} else {
				adds := strings.Count(p.Diff, "\n+")
				dels := strings.Count(p.Diff, "\n-")
				fmt.Printf("  📝 %s (+%d, -%d)\n", p.File, adds, dels)
			}
		}
	}

	// Write new/full files
	if len(prop.Files) > 0 {
		for path, content := range prop.Files {
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
	if len(prop.Commands) > 0 {
		fmt.Println()
		for _, cmd := range prop.Commands {
			fmt.Printf("  🔧 Running: %s\n", cmd)
			out, exitCode := e.sandbox.Exec(cmd)
			if exitCode != 0 {
				errMsg := fmt.Sprintf("command '%s' failed (exit %d): %s", cmd, exitCode, strings.TrimSpace(out))
				fmt.Printf("     ⚠️  exit %d: %s\n", exitCode, strings.TrimSpace(firstLines(out, 3)))
				errors = append(errors, errMsg)
			} else {
				fmt.Printf("     ✅ done\n")
			}
		}
	}

	if len(prop.Files) == 0 && len(prop.Commands) == 0 && len(prop.Patches) == 0 {
		fmt.Println("⚠️  No files, patches, or commands found in proposal.")
	}

	return errors
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

// ─── Git operations ──────────────────────────────────

// gitSetup creates or checks out the branch
func (e *Engine) gitSetup() {
	// Check if we're in a git repo
	_, code := e.sandbox.Exec("git rev-parse --is-inside-work-tree")
	if code != 0 {
		// Init a new repo
		fmt.Println("📦 Initializing git repo...")
		e.sandbox.Exec("git init")
		e.sandbox.Exec("git add -A")
		e.sandbox.Exec(`git commit -m "initial commit" --allow-empty`)
	}

	// Check current branch
	out, code := e.sandbox.Exec("git rev-parse --abbrev-ref HEAD")
	currentBranch := strings.TrimSpace(out)
	if code == 0 && currentBranch == e.config.Branch {
		fmt.Printf("📌 Already on branch: %s\n", e.config.Branch)
		return
	}

	// Try checkout existing, or create new
	_, code = e.sandbox.Exec(fmt.Sprintf("git checkout %s 2>/dev/null || git checkout -b %s", e.config.Branch, e.config.Branch))
	if code == 0 {
		fmt.Printf("📌 Branch: %s\n", e.config.Branch)
	} else {
		fmt.Printf("⚠️  Failed to checkout branch %s\n", e.config.Branch)
	}
}

// gitRebase rebases current branch on base
func (e *Engine) gitRebase() {
	fmt.Printf("🔀 Rebasing on %s...\n", e.config.Base)

	// Stash any uncommitted changes first
	e.sandbox.Exec("git stash")

	out, code := e.sandbox.Exec(fmt.Sprintf("git rebase %s", e.config.Base))
	if code != 0 {
		if strings.Contains(out, "CONFLICT") {
			fmt.Printf("⚠️  Rebase conflict detected — aborting rebase\n")
			lines := strings.Split(strings.TrimSpace(out), "\n")
			for i, l := range lines {
				if i >= 5 {
					break
				}
				fmt.Printf("     %s\n", l)
			}
			e.sandbox.Exec("git rebase --abort")
		} else {
			fmt.Printf("⚠️  Rebase failed: %s\n", firstLines(out, 3))
			e.sandbox.Exec("git rebase --abort")
		}
	} else {
		fmt.Println("  ✅ Rebase clean")
	}

	// Pop stash if we stashed
	e.sandbox.Exec("git stash pop 2>/dev/null")
}

// gitCommit stages all changes and commits
func (e *Engine) gitCommit(attempt int) {
	// Stage everything
	e.sandbox.Exec("git add -A")

	// Check if there's anything to commit
	out, code := e.sandbox.Exec("git status --porcelain")
	if code == 0 && strings.TrimSpace(out) == "" {
		fmt.Println("📦 Nothing to commit")
		return
	}

	// Generate commit message
	msg := fmt.Sprintf("%s: attempt %d", e.config.CommitPrefix, attempt)

	// Try to make a more descriptive message from the changes
	diffStat, _ := e.sandbox.Exec("git diff --cached --stat")
	diffLines := strings.Split(strings.TrimSpace(diffStat), "\n")
	if len(diffLines) > 0 {
		// Count files changed
		lastLine := diffLines[len(diffLines)-1]
		if strings.Contains(lastLine, "changed") {
			msg = fmt.Sprintf("%s: attempt %d — %s", e.config.CommitPrefix, attempt, strings.TrimSpace(lastLine))
		}
	}

	_, code = e.sandbox.Exec(fmt.Sprintf("git commit -m %q", msg))
	if code == 0 {
		fmt.Printf("📦 %s\n", msg)

		// Show the short log
		logOut, _ := e.sandbox.Exec("git log --oneline -1")
		fmt.Printf("   %s\n", strings.TrimSpace(logOut))
	}
}

// Patch represents a unified diff for an existing file
type Patch struct {
	File string
	Diff string
}

// Proposal holds parsed AI response — files, patches, and commands
type Proposal struct {
	Files    map[string]string // new or full replacement files
	Patches  []Patch           // unified diffs for existing files
	Commands []string          // shell commands to run after apply
}

// ParseProposal extracts files, patches, and commands from an AI response.
// Supports formats:
//  1. Patch: {"patches": [...], "files": {...}, "commands": [...]}
//  2. Full:  {"files": {path: content}, "commands": ["cmd1"]}
//  3. Legacy: {path: content}
func ParseProposalFull(response string) *Proposal {
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

	prop := &Proposal{
		Files: make(map[string]string),
	}

	// Try standard JSON unmarshal first — handles all escaping correctly
	var structured struct {
		Files    map[string]string `json:"files"`
		Patches  []struct {
			File string `json:"file"`
			Diff string `json:"diff"`
		} `json:"patches"`
		Commands []string `json:"commands"`
	}

	if err := json.Unmarshal([]byte(response), &structured); err == nil {
		prop.Files = structured.Files
		if prop.Files == nil {
			prop.Files = make(map[string]string)
		}
		for _, p := range structured.Patches {
			if p.File != "" && p.Diff != "" {
				prop.Patches = append(prop.Patches, Patch{File: p.File, Diff: p.Diff})
			}
		}
		prop.Commands = structured.Commands
		return prop
	}

	// Fallback: try legacy flat format {path: content}
	var flat map[string]string
	if err := json.Unmarshal([]byte(response), &flat); err == nil {
		prop.Files = flat
		return prop
	}

	// Last resort: custom parser for malformed JSON
	if strings.Contains(response, `"files"`) {
		files, commands := parseNewFormat(response)
		prop.Files = files
		prop.Commands = commands
	} else {
		prop.Files = ParseFiles(response)
	}

	return prop
}

// parsePatchesArray extracts patches from "patches": [{"file": "...", "diff": "..."}]
func parsePatchesArray(response string) []Patch {
	var patches []Patch

	patchesIdx := strings.Index(response, `"patches"`)
	if patchesIdx < 0 {
		return nil
	}

	rest := response[patchesIdx+9:]
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return nil
	}
	rest = rest[colonIdx+1:]

	bracketStart := strings.Index(rest, "[")
	if bracketStart < 0 {
		return nil
	}

	// Find matching ]
	depth := 0
	inStr := false
	esc := false
	end := -1
	for i := bracketStart; i < len(rest); i++ {
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
			if c == '[' {
				depth++
			} else if c == ']' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
	}

	if end < 0 {
		return nil
	}

	arrStr := rest[bracketStart+1 : end]

	// Extract each {file, diff} object
	for {
		objStart := strings.Index(arrStr, "{")
		if objStart < 0 {
			break
		}

		// Find matching }
		objDepth := 0
		objInStr := false
		objEsc := false
		objEnd := -1
		for i := objStart; i < len(arrStr); i++ {
			c := arrStr[i]
			if objEsc {
				objEsc = false
				continue
			}
			if c == '\\' && objInStr {
				objEsc = true
				continue
			}
			if c == '"' {
				objInStr = !objInStr
			}
			if !objInStr {
				if c == '{' {
					objDepth++
				} else if c == '}' {
					objDepth--
					if objDepth == 0 {
						objEnd = i
						break
					}
				}
			}
		}

		if objEnd < 0 {
			break
		}

		objStr := arrStr[objStart : objEnd+1]
		arrStr = arrStr[objEnd+1:]

		// Extract "file" and "diff" from the object
		file := extractJSONString(objStr, "file")
		diff := extractJSONString(objStr, "diff")

		if file != "" && diff != "" {
			// Unescape the diff
			diff = strings.ReplaceAll(diff, `\n`, "\n")
			diff = strings.ReplaceAll(diff, `\t`, "\t")
			diff = strings.ReplaceAll(diff, `\"`, `"`)
			diff = strings.ReplaceAll(diff, `\\`, `\`)
			patches = append(patches, Patch{File: file, Diff: diff})
		}
	}

	return patches
}

// extractJSONString pulls a string value for a given key from a JSON-like string
func extractJSONString(obj, key string) string {
	search := `"` + key + `"`
	idx := strings.Index(obj, search)
	if idx < 0 {
		return ""
	}
	rest := obj[idx+len(search):]
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return ""
	}
	rest = rest[colonIdx+1:]

	// Find opening quote
	quoteStart := strings.Index(rest, `"`)
	if quoteStart < 0 {
		return ""
	}
	rest = rest[quoteStart+1:]

	// Find closing quote (respecting escapes)
	var result strings.Builder
	esc := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if esc {
			switch c {
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case '"':
				result.WriteByte('"')
			case '\\':
				result.WriteByte('\\')
			default:
				result.WriteByte('\\')
				result.WriteByte(c)
			}
			esc = false
			continue
		}
		if c == '\\' {
			esc = true
			continue
		}
		if c == '"' {
			return result.String()
		}
		result.WriteByte(c)
	}
	return result.String()
}

// ParseProposal extracts files and commands (backward compatible wrapper)
func ParseProposal(response string) (map[string]string, []string) {
	prop := ParseProposalFull(response)
	return prop.Files, prop.Commands
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
