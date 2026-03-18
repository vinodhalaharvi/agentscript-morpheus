package review

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/claude"
	"github.com/vinodhalaharvi/agentscript/pkg/openai"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// GeminiReviewer is a functional field seam — same pattern as ReactGenerator.
// Gemini's GenerateContent doesn't match Reviewer directly because it's on
// a concrete type, so we inject it as a function value from registry.go.
type GeminiReviewer func(ctx context.Context, prompt string) (string, error)

// Plugin wires Claude + Gemini + GPT-4 into the review debate.
type Plugin struct {
	claude  *claude.ClaudeClient
	gemini  GeminiReviewer // injected from registry — Gemini seam
	openai  *openai.Client
	verbose bool
}

// NewPlugin creates the review plugin.
// gemini is injected as a closure — same pattern as Executor in pkg/agent.
func NewPlugin(
	cl *claude.ClaudeClient,
	gemini GeminiReviewer,
	oai *openai.Client,
	verbose bool,
) *Plugin {
	return &Plugin{
		claude:  cl,
		gemini:  gemini,
		openai:  oai,
		verbose: verbose,
	}
}

func (p *Plugin) Name() string { return "review" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		// codereview "3"          — 3 rounds, all available models
		// codereview              — 2 rounds default
		"codereview": p.codeReview,

		// codereview_focus "security" "3" — focused review
		"codereview_focus": p.codeReviewFocus,
	}
}

// codeReview runs a multi-model code review on piped input.
// Usage: read "main.go" >=> codereview "3"
func (p *Plugin) codeReview(ctx context.Context, args []string, input string) (string, error) {
	return p.runReview(ctx, args, input, "")
}

// codeReviewFocus runs a focused code review on a specific aspect.
// Usage: read "main.go" >=> codereview_focus "security" "2"
func (p *Plugin) codeReviewFocus(ctx context.Context, args []string, input string) (string, error) {
	focus := plugin.Arg(args, 0)
	// Shift args so rounds is now args[0] for runReview
	shifted := args
	if len(args) > 1 {
		shifted = args[1:]
	} else {
		shifted = []string{}
	}
	return p.runReview(ctx, shifted, input, focus)
}

func (p *Plugin) runReview(ctx context.Context, args []string, input, focus string) (string, error) {
	code := strings.TrimSpace(input)
	if code == "" {
		return "", fmt.Errorf("codereview requires piped code — use: read \"file.go\" >=> codereview")
	}

	// Parse rounds
	maxRounds := 2
	if r := plugin.Arg(args, 0); r != "" {
		n, err := strconv.Atoi(r)
		if err != nil || n < 1 || n > 5 {
			return "", fmt.Errorf("codereview: rounds must be 1-5, got %q", r)
		}
		maxRounds = n
	}

	// Build reviewer list from available clients
	reviewers := p.buildReviewers()
	if len(reviewers) == 0 {
		return "", fmt.Errorf("codereview: no AI clients configured — need CLAUDE_API_KEY, GEMINI_API_KEY, or OPENAI_API_KEY")
	}

	// Moderator is Claude if available, else first reviewer
	var moderator Reviewer
	if p.claude != nil {
		moderator = p.claude.Chat
	} else {
		moderator = reviewers[0].Reviewer
	}

	focusMsg := ""
	if focus != "" {
		focusMsg = fmt.Sprintf(" (focus: %s)", focus)
	}
	fmt.Printf("\n🔍 Code Review Forum%s\n", focusMsg)
	fmt.Printf("   Reviewers: %s\n", reviewerNames(reviewers))
	fmt.Printf("   Max rounds: %d\n", maxRounds)
	fmt.Printf("   Code: %d lines\n\n", strings.Count(code, "\n")+1)

	cfg := Config{
		Reviewers: reviewers,
		MaxRounds: maxRounds,
		Focus:     focus,
		Moderator: moderator,
	}

	result, err := Run(ctx, code, cfg)
	if err != nil {
		return "", err
	}

	return result.Format(), nil
}

// buildReviewers constructs the reviewer list from available clients.
// Gracefully degrades — works with 1, 2, or all 3 models.
func (p *Plugin) buildReviewers() []ReviewerConfig {
	var reviewers []ReviewerConfig

	if p.claude != nil {
		reviewers = append(reviewers, ReviewerConfig{
			Name:     "Claude (Anthropic)",
			Reviewer: p.claude.Chat,
		})
	}

	if p.gemini != nil {
		reviewers = append(reviewers, ReviewerConfig{
			Name:     "Gemini (Google)",
			Reviewer: Reviewer(p.gemini),
		})
	}

	if p.openai != nil {
		reviewers = append(reviewers, ReviewerConfig{
			Name:     "GPT-4o (OpenAI)",
			Reviewer: p.openai.Chat,
		})
	}

	return reviewers
}

func reviewerNames(reviewers []ReviewerConfig) string {
	names := make([]string, len(reviewers))
	for i, r := range reviewers {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}
