package whatsapp

import (
	"context"
	"fmt"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps WhatsAppClient as a Plugin.
type Plugin struct {
	client *WhatsAppClient
}

// NewPlugin creates a whatsapp plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{client: NewWhatsAppClient(verbose)}
}

func (p *Plugin) Name() string { return "whatsapp" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"whatsapp": p.whatsapp,
	}
}

func (p *Plugin) whatsapp(ctx context.Context, args []string, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("whatsapp needs piped content")
	}
	to := plugin.Arg(args, 0)
	return p.client.Send(ctx, to, input)
}
