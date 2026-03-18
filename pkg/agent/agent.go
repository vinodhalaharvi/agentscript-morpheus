// Package agent provides a self-bootstrapping agent command.
// It takes a natural language goal, asks Claude to generate a
// Morpheus DSL pipeline, shows it to the user for confirmation,
// then executes it.
//
// Seams in play:
//   - Claude client  → reused from pkg/claude (zero new infrastructure)
//   - DSL execution  → Executor functional field (same pattern as ReactGenerator)
//   - Confirmation   → stdin prompt — human always in the loop
package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/claude"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Executor is the seam between the agent plugin and the runtime.
// pkg/agent cannot import internal/agentscript (Go's internal package rule).
// So the runtime injects itself as this function type from registry.go —
// the exact same pattern as ReactGenerator in pkg/github/plugin.go.
type Executor func(ctx context.Context, dsl string) (string, error)

// Plugin is the agent plugin.
type Plugin struct {
	claude   *claude.ClaudeClient
	executor Executor
	verbose  bool
}

// NewPlugin creates an agent plugin.
// claude: the Claude client (already instantiated in runtime)
// executor: r.RunDSL injected as a closure from registry.go
func NewPlugin(cl *claude.ClaudeClient, executor Executor, verbose bool) *Plugin {
	return &Plugin{
		claude:   cl,
		executor: executor,
		verbose:  verbose,
	}
}

func (p *Plugin) Name() string { return "agent" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"agent": p.agent,
	}
}

// agent is the main command.
// Usage: agent "Research top 3 Go web frameworks and email me a comparison"
//
// Flow:
//  1. Send goal + full grammar context to Claude
//  2. Claude returns a Morpheus DSL pipeline
//  3. Show the pipeline to the user
//  4. Require confirmation (y/N) — non-negotiable
//  5. Execute the pipeline via the Executor seam
//  6. Return the result
func (p *Plugin) agent(ctx context.Context, args []string, input string) (string, error) {
	goal := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if goal == "" {
		return "", fmt.Errorf("agent requires a goal — e.g. agent \"research Go frameworks and email me\"")
	}

	fmt.Printf("\n🤖 Agent: generating pipeline for: %q\n", goal)

	// Step 1: Ask Claude to generate the DSL
	dsl, err := p.claude.Chat(ctx, buildAgentPrompt(goal))
	if err != nil {
		return "", fmt.Errorf("agent: Claude failed to generate pipeline: %w", err)
	}

	dsl = cleanDSL(dsl)

	if p.verbose {
		fmt.Printf("[agent] generated DSL:\n%s\n", dsl)
	}

	// Step 2: Show the pipeline
	fmt.Printf("\n📋 Generated Pipeline:\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("%s\n", dsl)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Step 3: Confirm — always required
	fmt.Printf("\n⚠️  Execute this pipeline? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		fmt.Printf("❌ Cancelled.\n")
		return "", fmt.Errorf("agent: pipeline cancelled by user")
	}

	fmt.Printf("✅ Executing...\n\n")

	// Step 4: Execute via the Executor seam
	return p.executor(ctx, dsl)
}

// buildAgentPrompt constructs the prompt that gives Claude the full grammar
// context and asks it to generate a pipeline for the given goal.
func buildAgentPrompt(goal string) string {
	return fmt.Sprintf(`You are an expert at writing Morpheus AgentScript DSL pipelines.

## DSL Grammar

A pipeline chains commands with >=> (Kleisli/sequential composition).
Parallel execution uses ( cmd1 <*> cmd2 <*> cmd3 ) — branches run concurrently.
Results from parallel blocks must be combined with >=> merge before further processing.

## Available Commands

### AI & Search
ask "prompt"                           — ask Gemini (uses piped context)
summarize                              — summarize piped input
analyze "focus"                        — analyze piped input  
translate "language"                   — translate piped input
perplexity "query"                     — AI web search with citations (fast)
perplexity_pro "query"                — deeper AI search (slower, accurate)
perplexity_recent "query" "week"      — time-filtered: hour/day/week/month
perplexity_domain "query" "site.com"  — domain-restricted search

### Data
search "query"            — web search
news "query"              — news articles
news_headlines "category" — top headlines
stock "AAPL,GOOG"         — stock prices
crypto "BTC,ETH"          — crypto prices
weather "location"        — weather
rss "alias-or-url"        — RSS feed (aliases: golang, google-ai, techcrunch)
job_search "q" "loc" "type" — job listings

### Google Workspace
email "to@email.com"      — send email with piped content
calendar "event info"     — create calendar event
drive_save "path"         — save to Google Drive
doc_create "title"        — create Google Doc

### Notifications
notify "slack"            — send to Slack
whatsapp "phone"          — send WhatsApp

### Multimodal
image_generate "prompt"   — generate image
video_generate "prompt"   — generate video
text_to_speech "voice"    — text to audio

### File
save "filename.md"        — save to local file
read "filename"           — read from file
merge                     — combine parallel results

## Rules
1. >=> chains sequential stages
2. ( a <*> b <*> c ) runs branches in parallel — must be followed by >=> merge
3. String args use double quotes
4. Terminal commands: email, notify, save, whatsapp
5. If goal mentions an email address, use it. Otherwise use save "output.md"

## Examples

Simple:
perplexity "Go 1.24 features" >=> summarize >=> save "go-news.md"

Parallel research:
(
  perplexity "Rust async 2026"
  <*> perplexity "Go async 2026"
)
>=> merge
>=> ask "Compare Rust and Go async models. Which is better for what use cases?"
>=> email "user@example.com"

Morning briefing:
(
  perplexity_recent "tech news" "day"
  <*> stock "NVDA,MSFT,GOOG"
  <*> weather "San Francisco"
)
>=> merge
>=> ask "Give me a concise CTO morning briefing"
>=> notify "slack"

## Goal

Generate a Morpheus DSL pipeline for:
%s

OUTPUT: Only the raw DSL. No explanation, no markdown fences, no comments.
If an email is needed but not specified in the goal, use save "output.md" instead.`, goal)
}

// cleanDSL strips markdown fences Claude might wrap around the DSL.
func cleanDSL(raw string) string {
	s := strings.TrimSpace(raw)
	for _, fence := range []string{"```agentscript", "```morpheus", "```dsl", "```"} {
		s = strings.TrimPrefix(s, fence)
	}
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
