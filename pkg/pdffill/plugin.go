package pdffill

import (
	"context"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps PDF form filling as a Plugin.
type Plugin struct {
	reasoner Reasoner
	verbose  bool
}

// NewPlugin creates a pdffill plugin.
// reasoner is the AI backend (Gemini or Claude) — injected by the registry.
func NewPlugin(reasoner Reasoner, verbose bool) *Plugin {
	return &Plugin{
		reasoner: reasoner,
		verbose:  verbose,
	}
}

func (p *Plugin) Name() string { return "pdffill" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"pdf_fields": p.pdfFields,
		"pdf_fill":   p.pdfFill,
	}
}

func (p *Plugin) pdfFields(ctx context.Context, args []string, input string) (string, error) {
	pdfPath, err := plugin.RequireArg(args, 0, "pdf_path")
	if err != nil {
		return "", err
	}
	return ExtractFields(ctx, pdfPath, p.verbose)
}

func (p *Plugin) pdfFill(ctx context.Context, args []string, input string) (string, error) {
	pdfPath, err := plugin.RequireArg(args, 0, "pdf_path")
	if err != nil {
		return "", err
	}
	dataPath := plugin.Arg(args, 1)
	return FillForm(ctx, p.reasoner, pdfPath, dataPath, input, p.verbose)
}
