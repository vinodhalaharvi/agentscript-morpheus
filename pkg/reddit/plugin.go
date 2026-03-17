package reddit

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps RedditClient as a Plugin.
type Plugin struct {
	client *RedditClient
	cache  *cache.Cache
}

// NewPlugin creates a reddit plugin.
func NewPlugin(c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{
		client: NewRedditClient(verbose),
		cache:  c,
	}
}

func (p *Plugin) Name() string { return "reddit" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"reddit": p.reddit,
	}
}

func (p *Plugin) reddit(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("reddit requires a subreddit or search query")
	}
	rawArgs := []string{query}
	if arg2 := plugin.Arg(args, 1); arg2 != "" {
		rawArgs = append(rawArgs, arg2)
	}
	isSub, q, sort := ParseRedditArgs(rawArgs...)
	return cache.CachedGet(p.cache, "reddit", q+sort, cache.CacheTTLReddit, func() (string, error) {
		var posts []RedditPost
		var err error
		if isSub {
			posts, err = p.client.SearchSubreddit(ctx, q, sort, 10)
		} else {
			posts, err = p.client.SearchReddit(ctx, q, sort, 10)
		}
		if err != nil {
			return "", err
		}
		return FormatRedditPosts(posts, q), nil
	})
}
