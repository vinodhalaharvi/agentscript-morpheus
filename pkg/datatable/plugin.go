package datatable

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin exposes the render command to the AgentScript DSL.
type Plugin struct {
	verbose bool
}

// NewPlugin creates a datatable render plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{verbose: verbose}
}

func (p *Plugin) Name() string { return "datatable" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"render": p.render,
	}
}

func (p *Plugin) render(_ context.Context, args []string, input string) (string, error) {
	dslPath := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if dslPath == "" {
		return "", fmt.Errorf("render requires a .table DSL file path\n\n  Usage:\n    render \"network.table\"\n    render \"network.table\" \"output.html\"\n    render \"network.table\" \"data.json\" \"output.html\"")
	}

	// Detect 3-arg form: render "dsl" "datafile" "output.html"
	// vs 2-arg form:     render "dsl" "output.html"
	// Heuristic: if args[1] ends with .json or .jsonl it's a datafile arg.
	var explicitDataFile, outPath string
	arg1 := plugin.Arg(args, 1)
	arg2 := plugin.Arg(args, 2)

	if arg2 != "" {
		// 3-arg form: args[1] = datafile, args[2] = output
		explicitDataFile = arg1
		outPath = arg2
	} else if strings.HasSuffix(strings.ToLower(arg1), ".json") ||
		strings.HasSuffix(strings.ToLower(arg1), ".jsonl") {
		// 2-arg form where second arg looks like a data file, not an html output
		explicitDataFile = arg1
	} else {
		// 2-arg form: args[1] = output path
		outPath = arg1
	}

	if outPath == "" {
		base := filepath.Base(dslPath)
		ext := filepath.Ext(base)
		outPath = strings.TrimSuffix(base, ext) + ".html"
	}

	td, err := ParseFile(dslPath)
	if err != nil {
		return "", fmt.Errorf("render: DSL parse error in %s: %w", dslPath, err)
	}

	// JSON data: explicit datafile arg > piped input > source URL in .table
	jsonData := ""
	if td.Source.Type == SourceStatic {
		if explicitDataFile != "" {
			if raw, err := os.ReadFile(explicitDataFile); err == nil {
				jsonData = string(raw)
			} else {
				return "", fmt.Errorf("render: cannot read datafile %s: %w", explicitDataFile, err)
			}
		} else {
			trimmed := strings.TrimSpace(input)
			if trimmed != "" && trimmed != dslPath {
				jsonData = trimmed
			} else if td.Source.URL != "" {
				if raw, err := os.ReadFile(td.Source.URL); err == nil {
					jsonData = string(raw)
				}
			}
		}
	}

	html := Generate(td, jsonData)

	if err := os.WriteFile(outPath, []byte(html), 0644); err != nil {
		return "", fmt.Errorf("render: cannot write %s: %w", outPath, err)
	}

	if p.verbose {
		fmt.Printf("🎨 render: %s → %s (%d cols, %s source)\n",
			dslPath, outPath, len(td.Columns), td.Source.Type)
	}

	return outPath, nil
}
