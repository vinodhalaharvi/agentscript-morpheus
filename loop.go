package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LoopConfig holds configuration for loop execution
type LoopConfig struct {
	Separator string        // how to split input into items (default: newline)
	Delay     time.Duration // delay between iterations (rate limiting)
	MaxItems  int           // max items to process (0 = unlimited)
}

// DefaultLoopConfig returns sensible defaults
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		Separator: "\n",
		Delay:     500 * time.Millisecond,
		MaxItems:  0,
	}
}

// ParseLoopItems splits input into iterable items based on content structure
func ParseLoopItems(input string, separator string) []string {
	if separator == "" {
		separator = "\n"
	}

	var items []string

	switch separator {
	case "line", "lines", "\n":
		// Split by newlines, skip empty and header lines
		for _, line := range strings.Split(input, "\n") {
			line = strings.TrimSpace(line)
			// Skip empty lines, markdown headers, separators
			if line == "" || line == "---" || strings.HasPrefix(line, "#") {
				continue
			}
			// Skip table header separators
			if strings.HasPrefix(line, "|--") || strings.HasPrefix(line, "| #") || strings.HasPrefix(line, "| -") {
				continue
			}
			items = append(items, line)
		}

	case "section", "sections":
		// Split by "---" or "## " headers
		sections := strings.Split(input, "---")
		for _, section := range sections {
			section = strings.TrimSpace(section)
			if section != "" {
				items = append(items, section)
			}
		}

	case "csv", ",":
		for _, item := range strings.Split(input, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}

	case "paragraph", "paragraphs":
		for _, para := range strings.Split(input, "\n\n") {
			para = strings.TrimSpace(para)
			if para != "" {
				items = append(items, para)
			}
		}

	case "table", "rows":
		// Parse markdown table rows
		for _, line := range strings.Split(input, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "|") {
				continue
			}
			// Skip header separator rows
			if strings.Contains(line, "---") {
				continue
			}
			// Skip header row (first | # | or | Symbol | etc)
			items = append(items, line)
		}

	default:
		items = strings.Split(input, separator)
	}

	return items
}

// ForEachResult holds the result of processing one item
type ForEachResult struct {
	Index  int
	Item   string
	Output string
	Error  error
}

// ExecuteForEach processes each item through a pipeline
// The callback receives each item and its index, and should return the processed result
func ExecuteForEach(
	ctx context.Context,
	items []string,
	config LoopConfig,
	verbose bool,
	process func(ctx context.Context, item string, index int) (string, error),
) ([]ForEachResult, error) {

	if config.MaxItems > 0 && len(items) > config.MaxItems {
		items = items[:config.MaxItems]
	}

	var results []ForEachResult

	for i, item := range items {
		if verbose {
			fmt.Printf("[FOREACH] Processing item %d/%d: %s\n", i+1, len(items), truncate(item, 60))
		}

		output, err := process(ctx, item, i)
		results = append(results, ForEachResult{
			Index:  i,
			Item:   item,
			Output: output,
			Error:  err,
		})

		if err != nil && verbose {
			fmt.Printf("[FOREACH] Item %d error: %v\n", i+1, err)
		}

		// Rate limit between iterations
		if i < len(items)-1 && config.Delay > 0 {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			case <-time.After(config.Delay):
			}
		}
	}

	return results, nil
}

// FormatForEachResults combines results into a single output string
func FormatForEachResults(results []ForEachResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Processed %d items\n\n", len(results)))

	successes := 0
	failures := 0

	for _, r := range results {
		if r.Error != nil {
			failures++
			sb.WriteString(fmt.Sprintf("## Item %d ❌\n", r.Index+1))
			sb.WriteString(fmt.Sprintf("Input: %s\n", truncate(r.Item, 100)))
			sb.WriteString(fmt.Sprintf("Error: %v\n\n", r.Error))
		} else {
			successes++
			sb.WriteString(fmt.Sprintf("## Item %d ✓\n", r.Index+1))
			if r.Output != "" {
				sb.WriteString(r.Output + "\n\n")
			}
		}
		sb.WriteString("---\n\n")
	}

	summary := fmt.Sprintf("**Summary:** %d succeeded, %d failed out of %d total\n\n",
		successes, failures, len(results))

	return summary + sb.String()
}

// truncate shortens a string to max length
func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

// ParseForEachArgs parses the DSL arguments for foreach
// foreach "line" — split by lines (default)
// foreach "csv"  — split by commas
// foreach "section" — split by --- or ## headers
// foreach "table" — parse markdown table rows
func ParseForEachArgs(arg string) LoopConfig {
	config := DefaultLoopConfig()

	if arg != "" {
		config.Separator = strings.ToLower(strings.Trim(arg, "\"' "))
	}

	return config
}
