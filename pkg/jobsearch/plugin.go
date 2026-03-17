package jobsearch

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps JobSearcher as a Plugin.
type Plugin struct {
	searchKey string
	cache     *cache.Cache
	verbose   bool
}

// NewPlugin creates a jobsearch plugin.
func NewPlugin(searchKey string, c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{searchKey: searchKey, cache: c, verbose: verbose}
}

func (p *Plugin) Name() string { return "jobsearch" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"job_search": p.jobSearch,
	}
}

func (p *Plugin) jobSearch(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("job_search requires a query")
	}
	location := plugin.Arg(args, 1)
	empType := plugin.Arg(args, 2)

	searcher := NewJobSearcher(p.searchKey, p.verbose)
	cfg := JobSearchConfig{Query: query, Location: location, EmploymentType: empType, NumPages: 1}
	return cache.CachedGet(p.cache, "jobs", query+location, cache.CacheTTLJobs, func() (string, error) {
		jobs, err := searcher.Search(ctx, cfg)
		if err != nil {
			return "", err
		}
		return FormatJobResults(jobs), nil
	})
}
