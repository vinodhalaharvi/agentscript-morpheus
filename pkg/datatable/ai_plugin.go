package datatable

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Reasoner is the AI seam — same pattern as pdffill.
type Reasoner func(ctx context.Context, prompt string) (string, error)

// AIPlugin exposes the table_render command.
// Separate from Plugin so the existing render command is untouched.
type AIPlugin struct {
	reasoner Reasoner
	verbose  bool
}

// NewAIPlugin creates the AI-powered table render plugin.
func NewAIPlugin(reasoner Reasoner, verbose bool) *AIPlugin {
	return &AIPlugin{reasoner: reasoner, verbose: verbose}
}

func (p *AIPlugin) Name() string { return "datatable_ai" }

func (p *AIPlugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"table_render": p.tableRender,
	}
}

func (p *AIPlugin) tableRender(ctx context.Context, args []string, input string) (string, error) {
	title := plugin.Coalesce(args, 0, "Dashboard")
	sourceArg := plugin.Arg(args, 1)
	jsonData := strings.TrimSpace(input)

	if jsonData == "" {
		return "", fmt.Errorf("table_render requires JSON data piped in\n\n  Usage:\n    read \"data.json\" >=> table_render \"My Dashboard\"\n    read \"data.json\" >=> table_render \"Live\" \"ws://host/feed\"\n    read \"data.json\" >=> table_render \"Live\" \"sse://host/stream\"\n    read \"data.json\" >=> table_render \"Live\" \"rest://host/api poll=10\"")
	}

	if p.reasoner == nil {
		return "", fmt.Errorf("table_render requires an AI backend (CLAUDE_API_KEY or GEMINI_API_KEY)")
	}

	sourceType, sourceURL, pollSec := parseSourceArg(sourceArg)

	schemaSample := jsonData
	if len(schemaSample) > 4000 {
		schemaSample = schemaSample[:4000] + "\n... (truncated)"
	}

	if p.verbose {
		fmt.Fprintf(os.Stderr, "[table_render] Source: %s, Sending JSON schema to AI...\n", sourceType)
	}

	prompt := buildTablePrompt(title, schemaSample, sourceType, sourceURL, pollSec)
	aiResp, err := p.reasoner(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("table_render: AI failed: %w", err)
	}

	tableDSL := cleanTableResponse(aiResp)

	if p.verbose {
		fmt.Fprintf(os.Stderr, "[table_render] Generated .table DSL:\n%s\n", tableDSL)
	}

	parser := NewParser(tableDSL)
	td, err := parser.Parse()
	if err != nil {
		return "", fmt.Errorf("table_render: generated DSL parse error: %w\n\nGenerated DSL:\n%s", err, tableDSL)
	}

	embedData := jsonData
	if sourceType != "static" {
		embedData = ""
	}
	html := Generate(td, embedData)

	return html, nil
}

// parseSourceArg parses the second arg to detect source type.
func parseSourceArg(arg string) (sourceType, url string, pollSec int) {
	if arg == "" {
		return "static", "", 0
	}

	pollSec = 0
	parts := strings.Fields(arg)
	cleaned := parts[0]
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "poll=") {
			fmt.Sscanf(p, "poll=%d", &pollSec)
		}
	}

	switch {
	case strings.HasPrefix(cleaned, "ws://") || strings.HasPrefix(cleaned, "wss://"):
		return "ws", cleaned, 0
	case strings.HasPrefix(cleaned, "sse://"):
		return "sse", "https://" + strings.TrimPrefix(cleaned, "sse://"), 0
	case strings.HasPrefix(cleaned, "rest://"):
		url = "https://" + strings.TrimPrefix(cleaned, "rest://")
		if pollSec == 0 {
			pollSec = 30
		}
		return "rest", url, pollSec
	case strings.HasPrefix(cleaned, "http://") || strings.HasPrefix(cleaned, "https://"):
		if pollSec > 0 {
			return "rest", cleaned, pollSec
		}
		return "rest", cleaned, 30
	default:
		return "static", "", 0
	}
}

func buildTablePrompt(title, jsonSample, sourceType, sourceURL string, pollSec int) string {
	sourceLine := `source static`
	switch sourceType {
	case "rest":
		sourceLine = fmt.Sprintf(`source rest "%s" poll=%ds`, sourceURL, pollSec)
	case "sse":
		sourceLine = fmt.Sprintf(`source sse "%s"`, sourceURL)
	case "ws":
		sourceLine = fmt.Sprintf(`source ws "%s"`, sourceURL)
	}

	return `You generate .table DSL for the AgentScript datatable renderer. Analyze the JSON data and produce a .table spec.

JSON DATA (sample):
` + jsonSample + `

DSL FORMAT — output ONLY this format, nothing else:

table "` + title + `"

` + sourceLine + `

columns {
  json_field "Display Label" TYPE FLAGS
}

theme dark
search true
pagination 25
export csv

RULES:
1. Look at the JSON structure. If data is wrapped in a field like {"sites": [...]} add field=sites to the source line. If the JSON is a plain array, do not add a field= parameter.
2. For each field in the JSON objects, create a column line.
3. Column TYPE must be one of: text, number, badge, date, link
4. Use "badge" for status fields (health, status, state) and add color=health
5. Use "number" for numeric fields (latency, counts, days, prices, amounts) and add "range" flag
6. Use "date" for date/time fields
7. Use "text" for everything else
8. Add "sortable filterable" to most columns
9. Column format: field_name "Human Label" type sortable filterable [color=health] [range]
10. Output ONLY the .table DSL. No markdown, no backticks, no explanation.
11. Use the exact source line provided above — do NOT change the source type or URL.
12. Column labels must be plain text only — no special characters like #, @, $, or symbols. Use words like "Rank" instead of "#".`
}

func cleanTableResponse(resp string) string {
	resp = strings.TrimSpace(resp)
	fence := string([]byte{96, 96, 96})
	if strings.HasPrefix(resp, fence) {
		lines := strings.Split(resp, "\n")
		if len(lines) > 2 {
			start := 1
			end := len(lines) - 1
			if strings.TrimSpace(lines[end]) == fence {
				end = len(lines) - 1
			}
			resp = strings.Join(lines[start:end], "\n")
		}
	}
	return strings.TrimSpace(resp)
}
