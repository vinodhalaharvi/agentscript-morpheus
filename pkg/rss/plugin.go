package rss

import (
	"context"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps RSSClient as a Plugin.
type Plugin struct {
	client *RSSClient
	cache  *cache.Cache
}

// NewPlugin creates an RSS plugin.
func NewPlugin(c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{
		client: NewRSSClient(verbose),
		cache:  c,
	}
}

func (p *Plugin) Name() string { return "rss" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"rss": p.rss,
	}
}

func (p *Plugin) rss(ctx context.Context, args []string, input string) (string, error) {
	u := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if u == "" {
		return ListFeedShortcuts(), nil
	}
	return cache.CachedGet(p.cache, "rss", u, cache.CacheTTLRSS, func() (string, error) {
		items, title, err := p.client.FetchFeed(ctx, u, 10)
		if err != nil {
			return "", err
		}
		return FormatRSSItems(items, title), nil
	})
}
