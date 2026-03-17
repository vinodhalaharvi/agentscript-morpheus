package notify

import (
	"context"
	"fmt"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps NotifyClient as a Plugin.
type Plugin struct {
	client *NotifyClient
}

// NewPlugin creates a notify plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{client: NewNotifyClient(verbose)}
}

func (p *Plugin) Name() string { return "notify" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"notify": p.notify,
	}
}

func (p *Plugin) notify(ctx context.Context, args []string, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("notify needs piped content")
	}
	target := plugin.Arg(args, 0)
	return p.client.Send(ctx, input, target)
}
