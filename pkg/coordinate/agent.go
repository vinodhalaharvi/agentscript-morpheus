package coordinate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/claude"
	"github.com/vinodhalaharvi/agentscript/pkg/coordinate/blackboard"
	"github.com/vinodhalaharvi/agentscript/pkg/matching"
)

// Agent represents a single coordination participant. For Phase 1 every
// agent is Claude-backed; Ollama/other reasoners come later.
//
// Agents are subscribe-driven: they register patterns against the board
// and react to events. When an agent's subscription fires, its handler
// constructs a prompt, sends it to the reasoner, parses the JSON response,
// and writes the result back to the board.
type Agent struct {
	ID            string
	System        string // system prompt for the reasoner
	Session       *claude.Session
	Board         *blackboard.Board
	Subscriptions []AgentSubscription
}

// AgentSubscription describes what board events this agent reacts to
// and how. The handler receives the triggering event plus the bindings
// from the pattern match.
type AgentSubscription struct {
	// PatternSource is the raw pattern text (for diagnostics)
	PatternSource string
	// Pattern is the compiled match pattern
	Pattern matching.Pattern
	// KeyFilter (optional) pre-filters by key before pattern matching
	KeyFilter func(string) bool
	// Instruction is the task description handed to the agent when a
	// matching event arrives. Variables from the bindings are
	// substituted with their string values.
	Instruction string
}

// NewAgent constructs an agent. The claudeClient is used to start a
// session with the system prompt.
func NewAgent(id, systemPrompt string, claudeClient *claude.ClaudeClient, board *blackboard.Board) *Agent {
	session := claudeClient.NewSession()
	session.SystemPrompt = systemPrompt
	return &Agent{
		ID:      id,
		System:  systemPrompt,
		Session: session,
		Board:   board,
	}
}

// Subscribe attaches a subscription to this agent's board. The handler
// is wrapped to:
//  1. Substitute pattern bindings into the instruction text
//  2. Call the reasoner with the substituted instruction
//  3. Parse the JSON response
//  4. Apply the response as board writes (one entry per top-level key,
//     or the whole response under a synthesized key)
func (a *Agent) Subscribe(ctx context.Context, sub AgentSubscription) uint64 {
	a.Subscriptions = append(a.Subscriptions, sub)
	return a.Board.Subscribe(a.ID, sub.Pattern, sub.KeyFilter, func(ev blackboard.WriteEvent, b matching.Bindings) error {
		return a.handleEvent(ctx, sub, ev, b)
	})
}

// handleEvent is the per-event reaction logic. Keeps the subscribe
// method clean and testable.
func (a *Agent) handleEvent(ctx context.Context, sub AgentSubscription, ev blackboard.WriteEvent, bindings matching.Bindings) error {
	fmt.Printf("  🔔 agent=%s triggered by key=%s by=%s\n", a.ID, ev.Key, ev.By)

	// Don't react to own writes (avoid infinite loops)
	if ev.By == a.ID {
		fmt.Printf("     skip: own write\n")
		return nil
	}

	// Build the prompt: instruction + variable substitutions + event context
	prompt := buildPrompt(sub.Instruction, bindings, ev)
	fmt.Printf("     prompt bytes=%d\n", len(prompt))

	// Call the reasoner
	resp, err := a.Session.Chat(ctx, prompt)
	if err != nil {
		fmt.Printf("     ❌ chat error: %v\n", err)
		return fmt.Errorf("agent %s chat: %w", a.ID, err)
	}
	fmt.Printf("     response bytes=%d\n", len(resp))
	if len(resp) > 0 {
		preview := resp
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		fmt.Printf("     response preview: %s\n", preview)
	}

	// Parse JSON from response — reasoner is expected to return either
	// a single object {key: value} to write, or {writes: [{key, value}]}
	// for multiple writes, or {} / null to skip.
	writes, err := parseAgentResponse(resp)
	if err != nil {
		// Non-fatal — the agent produced unstructured output. Log and
		// continue; this is a model-quality issue not a coordination bug.
		fmt.Printf("     ⚠️  unparseable response: %v\n", err)
		return nil
	}
	fmt.Printf("     parsed %d writes\n", len(writes))

	// Apply writes
	for _, w := range writes {
		wrote, err := a.Board.Write(w.Key, w.Value, a.ID)
		if err != nil {
			fmt.Printf("     ⚠️  write %s: %v\n", w.Key, err)
			continue
		}
		if wrote {
			fmt.Printf("     📝 wrote %s\n", w.Key)
		} else {
			fmt.Printf("     ⊘ write to %s REJECTED by policy\n", w.Key)
		}
	}
	return nil
}

// buildPrompt constructs the prompt sent to the reasoner on each event.
// It includes the agent's instruction with $var substitutions, plus
// context about the triggering event and the current board state.
func buildPrompt(instruction string, bindings matching.Bindings, ev blackboard.WriteEvent) string {
	// Substitute $var references in the instruction
	substituted := instruction
	for name, val := range bindings {
		placeholder := "$" + name
		substituted = strings.ReplaceAll(substituted, placeholder, formatValue(val))
	}

	var sb strings.Builder
	sb.WriteString("## TASK\n")
	sb.WriteString(substituted)
	sb.WriteString("\n\n## TRIGGERING EVENT\n")
	sb.WriteString(fmt.Sprintf("Key: %s\n", ev.Key))
	sb.WriteString(fmt.Sprintf("Written by: %s (round %d)\n", ev.By, ev.Round))
	sb.WriteString(fmt.Sprintf("Value: %s\n", formatValue(ev.Value)))
	if ev.Previous != nil {
		sb.WriteString(fmt.Sprintf("Previous value: %s\n", formatValue(ev.Previous)))
	}

	sb.WriteString("\n## RESPONSE FORMAT\n")
	sb.WriteString(`Respond with a JSON object of the form:
  {"writes": [{"key": "...", "value": {...}}, ...]}
If you have nothing to contribute, respond with:
  {"writes": []}
Do not include any prose outside the JSON.`)

	return sb.String()
}

// formatValue converts a Value to a human-readable string for prompts.
func formatValue(v matching.Value) string {
	if v == nil {
		return "null"
	}
	switch vv := v.(type) {
	case string:
		return vv
	case float64:
		// Trim trailing zeros
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", vv), "0"), ".")
	case bool:
		if vv {
			return "true"
		}
		return "false"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// agentWrite is one key/value the agent wants to write to the board.
type agentWrite struct {
	Key   string
	Value matching.Value
}

// parseAgentResponse extracts writes from the agent's JSON response.
// Accepts two shapes:
//
//  1. {"writes": [{"key": ..., "value": ...}, ...]}
//  2. {"key1": {...}, "key2": {...}} — each top-level key is a write
//
// Returns empty slice (not error) for an empty writes list.
func parseAgentResponse(raw string) ([]agentWrite, error) {
	// Strip markdown code fences if present
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}

	// Shape 1: {"writes": [...]}
	if writesRaw, ok := parsed["writes"]; ok {
		writesArr, ok := writesRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("writes field is not an array")
		}
		var out []agentWrite
		for i, item := range writesArr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("writes[%d] is not an object", i)
			}
			key, ok := m["key"].(string)
			if !ok {
				return nil, fmt.Errorf("writes[%d].key missing or not a string", i)
			}
			out = append(out, agentWrite{Key: key, Value: m["value"]})
		}
		return out, nil
	}

	// Shape 2: each top-level key is a write
	var out []agentWrite
	for k, v := range parsed {
		out = append(out, agentWrite{Key: k, Value: v})
	}
	return out, nil
}
