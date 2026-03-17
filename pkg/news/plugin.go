package news

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps NewsClient as a Plugin.
type Plugin struct {
	searchKey string
	cache     *cache.Cache
	verbose   bool
}

// NewPlugin creates a news plugin.
func NewPlugin(searchKey string, c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{searchKey: searchKey, cache: c, verbose: verbose}
}

func (p *Plugin) Name() string { return "news" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"news":           p.news,
		"news_headlines": p.headlines,
	}
}

func (p *Plugin) news(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("news requires a query")
	}
	client := NewNewsClient(os.Getenv("GNEWS_API_KEY"), p.searchKey, p.verbose)
	return cache.CachedGet(p.cache, "news", query, cache.CacheTTLNews, func() (string, error) {
		articles, err := client.Search(ctx, query, 10)
		if err != nil {
			return "", err
		}
		return FormatNewsResults(articles, query), nil
	})
}

func (p *Plugin) headlines(ctx context.Context, args []string, input string) (string, error) {
	category := plugin.Arg(args, 0)
	if category == "" {
		category = "general"
	}
	client := NewNewsClient(os.Getenv("GNEWS_API_KEY"), p.searchKey, p.verbose)
	return cache.CachedGet(p.cache, "headlines", category, cache.CacheTTLNews, func() (string, error) {
		articles, err := client.TopHeadlines(ctx, category, 10)
		if err != nil {
			return "", err
		}
		return FormatNewsResults(articles, category+" headlines"), nil
	})
}
