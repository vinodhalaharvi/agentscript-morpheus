package coordinate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vinodhalaharvi/agentscript/pkg/claude"
	"github.com/vinodhalaharvi/agentscript/pkg/coordinate/blackboard"
	"github.com/vinodhalaharvi/agentscript/pkg/matching"
)

// Engine runs a coordinate block from start to convergence (or timeout).
type Engine struct {
	Config       *Config
	ClaudeClient *claude.ClaudeClient

	// Optional seed entries to write to the board before the agents start.
	// Used when the coordinate block receives piped input from >=>.
	SeedEntries []SeedEntry

	// Round delay — how long to wait between rounds to let subscriptions
	// process. For now a simple sleep; future versions can wait on a
	// condition variable tied to in-flight subscription count.
	RoundDelay time.Duration
}

// SeedEntry is an initial board entry written before any agent runs.
type SeedEntry struct {
	Key   string
	Value matching.Value
}

// NewEngine constructs an engine from a parsed Config.
func NewEngine(cfg *Config, claudeClient *claude.ClaudeClient) *Engine {
	return &Engine{
		Config:       cfg,
		ClaudeClient: claudeClient,
		RoundDelay:   500 * time.Millisecond,
	}
}

// Run executes the coordination until convergence or max rounds.
// Returns the witness JSON on success; error on timeout or failure.
func (e *Engine) Run(ctx context.Context) (string, error) {
	// --- Setup ---

	// Print header
	fmt.Println()
	fmt.Printf("🤝 Coordinate: %s\n", e.Config.Name)
	fmt.Printf("   Coordination: %s\n", e.Config.Coordination)
	fmt.Printf("   Convergence:  %s\n", e.Config.Convergence)
	fmt.Printf("   Agents:       %d\n", len(e.Config.Agents))
	fmt.Printf("   Max rounds:   %d\n", e.Config.MaxRounds)
	fmt.Printf("   Stability:    %d\n", e.Config.StabilityRounds)
	fmt.Println()

	// Create blackboard
	policy := parseWritePolicy(e.Config.WritePolicy)
	board := blackboard.NewBoard(policy)

	// ------------------------------------------------------------
	// Filesystem bridge: if a sandbox is configured, subscribe a
	// global handler that writes file-shaped blackboard entries to
	// disk. This is what makes coordinate actually DELIVER work —
	// agents produce code by writing to the board, and those writes
	// become real files on disk automatically.
	//
	// "File-shaped" heuristic:
	//   - Value is a string
	//   - Key doesn't start with __ (engine-internal keys)
	//   - Key contains "/" (nested path) OR ends with a known file
	//     extension
	// ------------------------------------------------------------
	if e.Config.Sandbox != "" {
		if err := os.MkdirAll(e.Config.Sandbox, 0755); err != nil {
			return "", fmt.Errorf("create sandbox dir: %w", err)
		}
		fmt.Printf("   sandbox: %s\n", e.Config.Sandbox)

		wildcardPat, _ := matching.Parse("_")
		board.Subscribe("__fsbridge__", wildcardPat, nil,
			func(ev blackboard.WriteEvent, _ matching.Bindings) error {
				if !looksLikeFilePath(ev.Key) {
					return nil
				}
				content, ok := ev.Value.(string)
				if !ok {
					return nil // not a string payload — skip
				}
				dst := filepath.Join(e.Config.Sandbox, ev.Key)
				if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
					fmt.Printf("   ⚠️  fs-bridge: mkdir %s: %v\n", filepath.Dir(dst), err)
					return nil
				}
				if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
					fmt.Printf("   ⚠️  fs-bridge: write %s: %v\n", dst, err)
					return nil
				}
				fmt.Printf("   💾 fs-bridge: wrote %s (%d bytes)\n", dst, len(content))
				return nil
			})
	}

	// Create agents and wire their subscriptions to the board
	agents := make([]*Agent, 0, len(e.Config.Agents))
	for _, ac := range e.Config.Agents {
		agent := NewAgent(ac.ID, ac.System, e.ClaudeClient, board)
		for _, s := range ac.Subscriptions {
			sub := AgentSubscription{
				PatternSource: s.PatternSrc,
				Pattern:       s.Pattern,
				Instruction:   s.Instruction,
				KeyFilter:     makeKeyFilter(s.KeyFilterGlob),
			}
			agent.Subscribe(ctx, sub)
			fmt.Printf("   subscribed: %s on key=%q pattern=%q\n",
				ac.ID,
				defaultStr(s.KeyFilterGlob, "<any>"),
				s.PatternSrc)
		}
		agents = append(agents, agent)
		fmt.Printf("   agent ready: %s\n", ac.ID)
	}
	fmt.Println()

	// Build convergence predicate
	pred, err := BuildConvergence(e.Config.Coordination, e.Config.Convergence, board, ConvergenceConfig{
		StabilityRounds:    e.Config.StabilityRounds,
		MaxRounds:          e.Config.MaxRounds,
		AlignmentThreshold: e.Config.AlignmentThreshold,
		Quorum:             e.Config.Quorum,
	})
	if err != nil {
		return "", fmt.Errorf("convergence: %w", err)
	}
	fmt.Printf("   predicate: %s\n\n", pred.Description())

	// --- Seed the board ---
	for _, s := range e.SeedEntries {
		if _, err := board.Write(s.Key, s.Value, "__seed__"); err != nil {
			return "", fmt.Errorf("seed %s: %w", s.Key, err)
		}
	}

	// --- Run rounds ---
	for round := 1; round <= e.Config.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		fmt.Printf("🔄 Round %d/%d\n", round, e.Config.MaxRounds)
		currentRound := board.NextRound()

		// Tick the agents — force each agent to consider the current state.
		// In pure event-driven mode, agents only react to writes. But the
		// FIRST round of a coordination run has nothing on the board, so
		// no subscriptions would ever fire. We synthesize a tick event per
		// round so agents can volunteer contributions.
		//
		// CRITICAL: NotifyTick dispatches WITHOUT counting as a write.
		// If we used board.Write here, the engine itself would always be
		// writing every round, making equilibrium impossible to reach.
		// Equilibrium must measure real agent activity — the engine's
		// internal plumbing must not count.
		tickKey := fmt.Sprintf("__tick__/%d", currentRound)
		board.NotifyTick(tickKey, map[string]interface{}{
			"round": float64(currentRound),
		})

		// Allow subscriptions to process. Synchronous handler model:
		// when an agent's subscription fires, it calls Claude, parses
		// the response, and calls board.Write() — all within the
		// dispatch call chain. By the time NotifyTick returns, the
		// handlers for this round have already run.
		time.Sleep(e.RoundDelay)

		// Check convergence BEFORE printing the round summary, so the
		// summary doesn't suggest we'll keep going when we're done.
		if pred.Satisfied() {
			fmt.Println()
			fmt.Printf("✨ Convergence reached at round %d\n", round)
			witness := pred.Witness()
			witnessJSON, _ := json.MarshalIndent(witness, "", "  ")
			return string(witnessJSON), nil
		}

		// Round summary. "Total writes" here means REAL agent writes
		// (ticks excluded). If this number isn't growing, agents are
		// quiet and convergence should fire soon.
		lastWriteRound := board.LastWriteRound()
		fmt.Printf("   %d agent writes total; last write was round %d\n",
			board.WriteCount(), lastWriteRound)
	}

	// Max rounds exhausted
	return "", fmt.Errorf("coordinate %q did not converge after %d rounds",
		e.Config.Name, e.Config.MaxRounds)
}

// parseWritePolicy maps string to enum.
func parseWritePolicy(s string) blackboard.WritePolicy {
	switch s {
	case "higher_confidence_wins":
		return blackboard.HigherConfidenceWins
	case "append_only":
		return blackboard.AppendOnly
	default:
		return blackboard.LastWriteWins
	}
}

// makeKeyFilter converts a glob like "cells/*" into a match function.
// For v1, only trailing * is supported: "prefix/*" matches anything
// starting with "prefix/". An empty string returns nil (no filter).
func makeKeyFilter(glob string) func(string) bool {
	if glob == "" {
		return nil
	}
	if len(glob) > 2 && glob[len(glob)-2:] == "/*" {
		prefix := glob[:len(glob)-1]
		return func(k string) bool {
			return len(k) >= len(prefix) && k[:len(prefix)] == prefix
		}
	}
	// Exact match
	return func(k string) bool { return k == glob }
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// looksLikeFilePath returns true if key resembles a filesystem path
// and should be bridged to disk. Heuristic:
//   - Reject engine-internal keys (start with "__")
//   - Accept if key contains "/" (nested path like cmd/x/main.go)
//   - Accept if key ends with a known source/config extension
func looksLikeFilePath(key string) bool {
	if strings.HasPrefix(key, "__") {
		return false
	}
	if strings.Contains(key, "/") {
		return true
	}
	// Leaf-only paths (no slash): recognize by extension
	exts := []string{
		".go", ".mod", ".sum",
		".yaml", ".yml", ".json", ".toml", ".xml",
		".md", ".txt", ".rst",
		".sh", ".bash",
		".sql", ".proto",
		".py", ".rs", ".ts", ".js",
		".dockerfile", ".Dockerfile",
		".env", ".gitignore",
		".html", ".css",
	}
	lower := strings.ToLower(key)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	// Common filenames without extensions
	switch key {
	case "Makefile", "Dockerfile", "LICENSE", "README", "CHANGELOG":
		return true
	}
	return false
}
