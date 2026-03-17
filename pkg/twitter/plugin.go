package twitter

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps TwitterClient as a Plugin.
type Plugin struct {
	client *TwitterClient
}

// NewPlugin creates a twitter plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{client: NewTwitterClient(verbose)}
}

func (p *Plugin) Name() string { return "twitter" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"twitter": p.twitter,
	}
}

func (p *Plugin) twitter(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("twitter requires a search query")
	}
	tweets, err := p.client.SearchRecent(ctx, query, 10)
	if err != nil {
		return "", err
	}
	return FormatTweets(tweets, query), nil
}
