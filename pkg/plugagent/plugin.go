// Package plugagent generates new AgentScript plugins from plain English.
//
// This is the "5 minutes to a new plugin" promise made real.
// Instead of reading docs and copying patterns manually, you describe
// what you want and Claude generates compilable plugin code grounded
// in the exact conventions of your codebase.
//
// Context strategy (the PureAST idea):
//  1. Read pkg/plugin/plugin.go    — the Plugin interface + CommandFunc + helpers
//  2. Read pkg/weather/plugin.go   — canonical simple plugin (one command)
//  3. Read pkg/news/plugin.go      — canonical multi-command plugin with cache
//  4. Read pkg/perplexity/plugin.go — canonical multi-command, no cache
//  5. Read internal/agentscript/registry.go — so Claude knows how to wire it
//  6. Read internal/agentscript/grammar.go  — so Claude knows the keyword rules
//
// All of this is real source, not prose descriptions. Claude gets the actual
// type signatures, function shapes, and import paths — so the generated code
// compiles first time.
//
// Seams:
//
//	Generator = func(ctx, prompt) (string, error) — Claude generates the code
//	FileWriter = func(path, content string) error  — write output files
package plugagent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Generator is the AI seam — Claude generates Go code from the prompt.
// Same signature as Reviewer, Reasoner, Executor — the universal pattern.
type Generator func(ctx context.Context, prompt string) (string, error)

// Plugin is the plug_agent plugin.
type Plugin struct {
	generator Generator
	repoRoot  string // path to agentscript-morpheus root
	verbose   bool
}

// NewPlugin creates a plug_agent plugin.
// generator: Claude.Chat injected from registry
// repoRoot:  path to the repo root (for reading source files as context)
func NewPlugin(generator Generator, repoRoot string, verbose bool) *Plugin {
	return &Plugin{
		generator: generator,
		repoRoot:  repoRoot,
		verbose:   verbose,
	}
}

func (p *Plugin) Name() string { return "plugagent" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		// plug_agent "a plugin that fetches Hacker News top stories"
		"plug_agent": p.plugAgent,
	}
}

// plugAgent generates a new plugin from a plain English description.
//
// Usage: plug_agent "a plugin that fetches Hacker News top stories and formats them"
//
// Flow:
//  1. Read key source files as AST context (real code, not prose)
//  2. Build a prompt with the context + description
//  3. Send to Claude — it generates pkg/<name>/<name>.go + pkg/<name>/plugin.go
//  4. Show the generated files to the user
//  5. Ask for confirmation before writing to disk
//  6. Write files + print the registry.go and grammar.go changes needed
func (p *Plugin) plugAgent(ctx context.Context, args []string, input string) (string, error) {
	description := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if description == "" {
		return "", fmt.Errorf("plug_agent requires a description — e.g. plug_agent \"fetch Hacker News top stories\"")
	}

	fmt.Printf("\n🔧 plug_agent: generating plugin for: %q\n", description)
	fmt.Printf("   Reading codebase context...\n")

	// Step 1: Read source files as context
	context_src, err := p.buildContext()
	if err != nil {
		return "", fmt.Errorf("plug_agent: failed to read codebase context: %w", err)
	}

	if p.verbose {
		fmt.Printf("[plug_agent] context: %d bytes from %d files\n", len(context_src.content), context_src.fileCount)
	}

	// Step 2: Generate the plugin
	fmt.Printf("   Sending to Claude with %d bytes of codebase context...\n", len(context_src.content))

	prompt := buildGenerationPrompt(description, context_src.content)
	generated, err := p.generator(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("plug_agent: Claude generation failed: %w", err)
	}

	// Step 3: Parse the generated output into files
	files, err := parseGeneratedOutput(generated)
	if err != nil {
		return "", fmt.Errorf("plug_agent: failed to parse generated output: %w", err)
	}

	// Step 4: Show the generated files
	fmt.Printf("\n📋 Generated Files:\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	for _, f := range files {
		fmt.Printf("\n// %s\n", f.path)
		fmt.Printf("%s\n", f.content)
		fmt.Printf("────────────────────────────────────────\n")
	}

	// Step 5: Confirm before writing
	fmt.Printf("\n⚠️  Write these files to disk? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		fmt.Printf("❌ Cancelled — files not written.\n")
		return generated, nil
	}

	// Step 6: Write files
	for _, f := range files {
		fullPath := filepath.Join(p.repoRoot, f.path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("plug_agent: failed to create dir %s: %w", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(f.content), 0644); err != nil {
			return "", fmt.Errorf("plug_agent: failed to write %s: %w", fullPath, err)
		}
		fmt.Printf("✅ Written: %s\n", f.path)
	}

	// Step 7: Print what still needs to be done manually
	fmt.Printf("\n📝 Manual steps remaining:\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	if len(files) > 0 {
		// Extract package name from first file path
		pkgName := extractPackageName(files)
		printManualSteps(pkgName)
	}

	result := fmt.Sprintf("Plugin generated: %d files written\n", len(files))
	for _, f := range files {
		result += fmt.Sprintf("  %s\n", f.path)
	}
	return result, nil
}

// contextResult holds the assembled source context.
type contextResult struct {
	content   string
	fileCount int
}

// buildContext reads the key source files and assembles them into
// a single context string for Claude. This is the PureAST idea —
// real source code as context, not prose descriptions.
func (p *Plugin) buildContext() (*contextResult, error) {
	// These files are the minimum context Claude needs to generate
	// a compilable plugin that follows your exact conventions.
	contextFiles := []struct {
		path    string
		purpose string
	}{
		{
			path:    "pkg/plugin/plugin.go",
			purpose: "THE PLUGIN CONTRACT — Plugin interface, CommandFunc type, Registry, Arg/Coalesce/RequireArg helpers",
		},
		{
			path:    "pkg/weather/plugin.go",
			purpose: "CANONICAL SIMPLE PLUGIN — one command, cache pattern, NewPlugin constructor shape",
		},
		{
			path:    "pkg/news/plugin.go",
			purpose: "CANONICAL MULTI-COMMAND PLUGIN — two commands, cache, env var for API key",
		},
		{
			path:    "pkg/perplexity/plugin.go",
			purpose: "CANONICAL MULTI-COMMAND NO-CACHE — shows SearchOptions pattern, command variants",
		},
		{
			path:    "internal/agentscript/registry.go",
			purpose: "COMPOSITION ROOT — how to wire a new plugin (import + Register call)",
		},
		{
			path:    "internal/agentscript/grammar.go",
			purpose: "GRAMMAR — where to add new keywords (Action list + Keyword lexer pattern)",
		},
	}

	var sb strings.Builder
	fileCount := 0

	for _, cf := range contextFiles {
		fullPath := filepath.Join(p.repoRoot, cf.path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			// Non-fatal — skip missing files with a warning
			fmt.Printf("   ⚠️  Could not read %s: %v\n", cf.path, err)
			continue
		}

		sb.WriteString(fmt.Sprintf("\n\n// ============================================================\n"))
		sb.WriteString(fmt.Sprintf("// FILE: %s\n", cf.path))
		sb.WriteString(fmt.Sprintf("// PURPOSE: %s\n", cf.purpose))
		sb.WriteString(fmt.Sprintf("// ============================================================\n"))
		sb.WriteString(string(data))
		fileCount++
	}

	return &contextResult{
		content:   sb.String(),
		fileCount: fileCount,
	}, nil
}

// generatedFile holds one output file.
type generatedFile struct {
	path    string
	content string
}

// buildGenerationPrompt constructs the prompt sent to Claude.
// This is the core of plug_agent — real source as context, clear output format.
func buildGenerationPrompt(description, sourceContext string) string {
	return fmt.Sprintf(`You are generating a new AgentScript plugin in Go.

## Your Task

Generate a complete, compilable AgentScript plugin for:
"%s"

## Codebase Context

Below is the ACTUAL SOURCE CODE from the AgentScript repository.
Study it carefully — your generated code must follow EXACTLY the same patterns,
import paths, naming conventions, and architectural decisions.

%s

## Generation Rules

1. **Module path**: always "github.com/vinodhalaharvi/agentscript/pkg/<name>"
2. **Plugin interface**: implement exactly Name() string and Commands() map[string]CommandFunc
3. **CommandFunc signature**: func(ctx context.Context, args []string, input string) (string, error)
4. **Arg helpers**: use plugin.Arg(), plugin.Coalesce(), plugin.RequireArg() — never index args directly
5. **Constructor**: NewPlugin(...) *Plugin — accept only what you need
6. **Two files**: <name>.go (client/logic) and plugin.go (DSL wiring) in pkg/<name>/
7. **HTTP clients**: use net/http with timeout (30s), check status codes
8. **Error messages**: prefix with package name — fmt.Errorf("<name>: ...")
9. **No generics at the Plugin boundary** — string-typed by design
10. **Compile on first try** — import exactly what you use, nothing more

## Output Format

Output EXACTLY this format — Claude will parse it to extract files:

FILE: pkg/<name>/<name>.go
`+"```go"+`
<complete client file content>
`+"```"+`

FILE: pkg/<name>/plugin.go
`+"```go"+`
<complete plugin file content>
`+"```"+`

REGISTRY_LINE: reg.Register(<name>.NewPlugin(<args>))
GRAMMAR_KEYWORDS: <comma-separated list of new command names>
GRAMMAR_LEXER: <pipe-separated list for the Keyword regex, longer first>

No other text. No explanation. Just the files and the wiring hints.`, description, sourceContext)
}

// parseGeneratedOutput parses Claude's response into files.
func parseGeneratedOutput(generated string) ([]generatedFile, error) {
	var files []generatedFile
	lines := strings.Split(generated, "\n")

	var currentPath string
	var currentContent strings.Builder
	inCodeBlock := false

	for _, line := range lines {
		if strings.HasPrefix(line, "FILE: ") {
			// Save previous file if any
			if currentPath != "" && currentContent.Len() > 0 {
				files = append(files, generatedFile{
					path:    currentPath,
					content: strings.TrimSpace(currentContent.String()),
				})
				currentContent.Reset()
			}
			currentPath = strings.TrimSpace(strings.TrimPrefix(line, "FILE: "))
			inCodeBlock = false
			continue
		}

		if strings.HasPrefix(line, "```go") {
			inCodeBlock = true
			continue
		}

		if line == "```" && inCodeBlock {
			inCodeBlock = false
			continue
		}

		// Skip non-code lines when not in a block
		if strings.HasPrefix(line, "REGISTRY_LINE:") ||
			strings.HasPrefix(line, "GRAMMAR_KEYWORDS:") ||
			strings.HasPrefix(line, "GRAMMAR_LEXER:") {
			continue
		}

		if currentPath != "" && inCodeBlock {
			currentContent.WriteString(line + "\n")
		}
	}

	// Save last file
	if currentPath != "" && currentContent.Len() > 0 {
		files = append(files, generatedFile{
			path:    currentPath,
			content: strings.TrimSpace(currentContent.String()),
		})
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found in Claude's output — raw output:\n%s", generated[:min(500, len(generated))])
	}

	return files, nil
}

// extractPackageName gets the package name from the first generated file path.
// pkg/hackernews/hackernews.go → "hackernews"
func extractPackageName(files []generatedFile) string {
	if len(files) == 0 {
		return "newplugin"
	}
	parts := strings.Split(files[0].path, "/")
	if len(parts) >= 2 {
		return parts[1] // pkg/<name>/...
	}
	return "newplugin"
}

// printManualSteps prints the two changes still needed after file generation:
// 1. Add import + Register call to registry.go
// 2. Add keyword(s) to grammar.go
func printManualSteps(pkgName string) {
	fmt.Printf(`
Two manual changes needed:

1. internal/agentscript/registry.go — add import and Register call:

   import "%s/pkg/%s"

   // Inside buildRegistry():
   reg.Register(%s.NewPlugin(...))

2. internal/agentscript/grammar.go — add to Action list AND Keyword pattern:

   Action list: add | "%s_command"
   Keyword pattern: add |%s_command before any prefix it shares

Then: make build
`, "github.com/vinodhalaharvi/agentscript", pkgName, pkgName, pkgName, pkgName)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
