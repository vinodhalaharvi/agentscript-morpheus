package agentscript

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/news"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// NewsPlugin handles "news" and "news_headlines" commands.
type NewsPlugin struct {
	searchKey string
	cache     *cache.Cache
	verbose   bool
}

func NewNewsPlugin(searchKey string, c *cache.Cache, verbose bool) *NewsPlugin {
	return &NewsPlugin{searchKey: searchKey, cache: c, verbose: verbose}
}

func (p *NewsPlugin) Name() string { return "news" }

func (p *NewsPlugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"news":           p.fetch,
		"news_headlines": p.headlines,
	}
}

func (p *NewsPlugin) client() *news.NewsClient {
	return news.NewNewsClient(os.Getenv("GNEWS_API_KEY"), p.searchKey, p.verbose)
}

func (p *NewsPlugin) fetch(ctx context.Context, args []string, input string) (string, error) {
	query := firstArg(args)
	if query == "" {
		query = strings.TrimSpace(input)
	}
	if query == "" {
		return "", fmt.Errorf("news requires a query")
	}
	return cache.CachedGet(p.cache, "news", query, cache.CacheTTLNews, func() (string, error) {
		articles, err := p.client().Search(ctx, query, 10)
		if err != nil {
			return "", err
		}
		return news.FormatNewsResults(articles, query), nil
	})
}

func (p *NewsPlugin) headlines(ctx context.Context, args []string, input string) (string, error) {
	category := argOr(args, 0, "general")
	return cache.CachedGet(p.cache, "headlines", category, cache.CacheTTLNews, func() (string, error) {
		articles, err := p.client().TopHeadlines(ctx, category, 10)
		if err != nil {
			return "", err
		}
		return news.FormatNewsResults(articles, category+" headlines"), nil
	})
}
