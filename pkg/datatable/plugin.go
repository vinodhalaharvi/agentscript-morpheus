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
		return "", fmt.Errorf("render requires a .table DSL file path\n\n  Usage:\n    render \"network.table\"\n    render \"network.table\" \"output.html\"")
	}

	outPath := plugin.Arg(args, 1)
	if outPath == "" {
		base := filepath.Base(dslPath)
		ext := filepath.Ext(base)
		outPath = strings.TrimSuffix(base, ext) + ".html"
	}

	td, err := ParseFile(dslPath)
	if err != nil {
		return "", fmt.Errorf("render: DSL parse error in %s: %w", dslPath, err)
	}

	// JSON data from piped input or local file
	jsonData := ""
	if td.Source.Type == SourceStatic {
		trimmed := strings.TrimSpace(input)
		if trimmed != "" && trimmed != dslPath {
			jsonData = trimmed
		} else if td.Source.URL != "" {
			if raw, err := os.ReadFile(td.Source.URL); err == nil {
				jsonData = string(raw)
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
