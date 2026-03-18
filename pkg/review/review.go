// Package review orchestrates a multi-model code review debate.
//
// Each model is a Reviewer — a named function type (Kleisli arrow).
// The orchestrator runs rounds of debate until consensus or max rounds.
// No model knows about the others — they only see the conversation history.
//
// The seam:
//
//	type Reviewer func(ctx context.Context, prompt string) (string, error)
//
// Claude, Gemini, GPT-4 all satisfy this with their .Chat/.GenerateContent
// methods — zero adapter code needed.
package review

import (
	"context"
	"fmt"
	"strings"
)

// Reviewer is the seam. Any LLM that can respond to a prompt is a Reviewer.
// Claude.Chat, Gemini.GenerateContent, OpenAI.Chat — all satisfy this.
type Reviewer func(ctx context.Context, prompt string) (string, error)

// ReviewerConfig names a Reviewer for display in the debate transcript.
type ReviewerConfig struct {
	Name     string
	Reviewer Reviewer
}

// RoundResult holds one model's response in one round.
type RoundResult struct {
	Reviewer string
	Round    int
	Response string
}

// DebateResult is the full output of the debate.
type DebateResult struct {
	Rounds    [][]RoundResult // [round][reviewer]
	Consensus string          // final synthesized consensus
	FinalCode string          // extracted final code if present
}

// Config controls the debate.
type Config struct {
	Reviewers []ReviewerConfig
	MaxRounds int
	Focus     string   // optional: "security", "performance", "readability"
	Moderator Reviewer // moderates and declares consensus — usually Claude
}

// Run executes the multi-model code review debate.
func Run(ctx context.Context, code string, cfg Config) (*DebateResult, error) {
	if len(cfg.Reviewers) == 0 {
		return nil, fmt.Errorf("review: at least one reviewer required")
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 3
	}
	if cfg.Moderator == nil {
		// Fall back to first reviewer as moderator
		cfg.Moderator = cfg.Reviewers[0].Reviewer
	}

	result := &DebateResult{}
	history := []RoundResult{}

	for round := 1; round <= cfg.MaxRounds; round++ {
		fmt.Printf("\n🔄 Round %d/%d\n", round, cfg.MaxRounds)

		roundResults := []RoundResult{}

		for _, rc := range cfg.Reviewers {
			fmt.Printf("  🤖 %s reviewing...\n", rc.Name)

			prompt := buildReviewPrompt(rc.Name, code, history, round, cfg.Focus)
			response, err := rc.Reviewer(ctx, prompt)
			if err != nil {
				return nil, fmt.Errorf("review: %s round %d failed: %w", rc.Name, round, err)
			}

			rr := RoundResult{
				Reviewer: rc.Name,
				Round:    round,
				Response: response,
			}
			roundResults = append(roundResults, rr)
			history = append(history, rr)

			fmt.Printf("  ✅ %s done\n", rc.Name)
		}

		result.Rounds = append(result.Rounds, roundResults)

		// Check consensus after round 1+
		if round >= 1 {
			fmt.Printf("\n⚖️  Checking consensus...\n")
			consensus, reached, err := checkConsensus(ctx, code, history, cfg.Moderator)
			if err != nil {
				return nil, fmt.Errorf("review: consensus check failed: %w", err)
			}
			if reached {
				fmt.Printf("✅ Consensus reached after round %d\n", round)
				result.Consensus = consensus
				result.FinalCode = extractCode(consensus)
				return result, nil
			}
			fmt.Printf("↩️  No consensus yet, continuing...\n")
		}
	}

	// Max rounds reached — force final synthesis
	fmt.Printf("\n📋 Max rounds reached — synthesizing final review...\n")
	final, err := synthesizeFinal(ctx, code, history, cfg.Moderator, cfg.Focus)
	if err != nil {
		return nil, fmt.Errorf("review: final synthesis failed: %w", err)
	}
	result.Consensus = final
	result.FinalCode = extractCode(final)
	return result, nil
}

// Format returns the full debate transcript as readable text.
func (d *DebateResult) Format() string {
	var sb strings.Builder

	for _, round := range d.Rounds {
		if len(round) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n## Round %d\n", round[0].Round))
		for _, r := range round {
			sb.WriteString(fmt.Sprintf("\n### %s\n%s\n", r.Reviewer, r.Response))
		}
	}

	sb.WriteString("\n## Consensus\n")
	sb.WriteString(d.Consensus)

	if d.FinalCode != "" {
		sb.WriteString("\n\n## Final Code\n```go\n")
		sb.WriteString(d.FinalCode)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

// --- Internal helpers ---

func buildReviewPrompt(name, code string, history []RoundResult, round int, focus string) string {
	var sb strings.Builder

	focusLine := ""
	if focus != "" {
		focusLine = fmt.Sprintf("\nFocus particularly on: **%s**\n", focus)
	}

	if round == 1 {
		// First round — fresh review
		sb.WriteString(fmt.Sprintf(`You are %s, participating in a multi-model code review forum.
%s
Review the following Go code. Be specific, technical, and constructive.
Identify bugs, design issues, missing error handling, performance concerns,
and suggest concrete improvements with code examples where helpful.

## Code to Review

`+"```go\n%s\n```"+`

Provide your review:`, name, focusLine, code))
	} else {
		// Subsequent rounds — respond to others
		sb.WriteString(fmt.Sprintf(`You are %s, in round %d of a multi-model code review forum.
%s
## Original Code

`+"```go\n%s\n```"+`

## Previous Review Rounds

`, name, round, focusLine, code))

		for _, h := range history {
			sb.WriteString(fmt.Sprintf("### %s (Round %d)\n%s\n\n", h.Reviewer, h.Round, h.Response))
		}

		sb.WriteString(`## Your Task

Review the other reviewers' feedback. 
- Agree with valid points (say so explicitly)
- Respectfully disagree where you see it differently (explain why)
- Add any new issues the others missed
- Update your recommendations based on the discussion

Be concise — focus on delta from round 1, not repeating everything.`)
	}

	return sb.String()
}

func checkConsensus(ctx context.Context, code string, history []RoundResult, moderator Reviewer) (string, bool, error) {
	var sb strings.Builder
	sb.WriteString(`You are moderating a multi-model code review forum.

## Original Code

` + "```go\n" + code + "\n```" + `

## Review Discussion

`)
	for _, h := range history {
		sb.WriteString(fmt.Sprintf("### %s (Round %d)\n%s\n\n", h.Reviewer, h.Round, h.Response))
	}

	sb.WriteString(`## Your Task

Assess whether the reviewers have reached consensus on the critical issues.

Consensus means:
- All reviewers agree on the major bugs/issues (if any)
- All reviewers agree on the key improvements needed
- No reviewer has an unresolved objection to another's point

Answer with EXACTLY this format:
CONSENSUS: YES or NO
REASON: one sentence explaining why

If YES, also provide:
SUMMARY: a 3-5 bullet point summary of what all reviewers agreed on`)

	response, err := moderator(ctx, sb.String())
	if err != nil {
		return "", false, err
	}

	reached := strings.Contains(strings.ToUpper(response), "CONSENSUS: YES")
	return response, reached, nil
}

func synthesizeFinal(ctx context.Context, code string, history []RoundResult, moderator Reviewer, focus string) (string, error) {
	var sb strings.Builder

	focusLine := ""
	if focus != "" {
		focusLine = fmt.Sprintf("\nPay special attention to: **%s**\n", focus)
	}

	sb.WriteString(fmt.Sprintf(`You are the moderator of a multi-model code review forum.
%s
## Original Code

`+"```go\n%s\n```"+`

## Full Review Discussion

`, focusLine, code))

	for _, h := range history {
		sb.WriteString(fmt.Sprintf("### %s (Round %d)\n%s\n\n", h.Reviewer, h.Round, h.Response))
	}

	sb.WriteString(`## Your Task

Synthesize all the feedback into a final authoritative code review.

Provide:
1. **Critical Issues** — must fix before production
2. **Important Improvements** — strongly recommended  
3. **Minor Suggestions** — nice to have
4. **Points of Agreement** — what all reviewers agreed on
5. **Final Revised Code** — incorporate all critical fixes and important improvements

For the final code, provide the complete revised file wrapped in ` + "```go" + ` code blocks.`)

	return moderator(ctx, sb.String())
}

// extractCode pulls the first Go code block from a response.
func extractCode(response string) string {
	start := strings.Index(response, "```go")
	if start == -1 {
		return ""
	}
	start += 5 // skip ```go
	end := strings.Index(response[start:], "```")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(response[start : start+end])
}
