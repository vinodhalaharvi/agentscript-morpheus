package perplexity

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps the Perplexity Client as a DSL Plugin.
//
// Commands exposed:
//
//	perplexity "query"                    — standard search
//	perplexity_pro "query"                — deeper search with sonar-pro
//	perplexity_recent "query" "week"      — time-filtered search (day/week/month)
//	perplexity_domain "query" "domain.com" — domain-restricted search
//
// The ReactGenerator seam from github/plugin.go shows how a functional field
// makes a plugin configurable without changing its interface.
// The perplexity plugin demonstrates the same principle with SearchOptions —
// each command variant is just a CommandFunc closure that captures different
// SearchOptions. No subclassing, no strategy pattern, just functions.
type Plugin struct {
	client *Client
}

// NewPlugin creates a Perplexity plugin.
// apiKey from PERPLEXITY_API_KEY env var.
// model can be "", "sonar", or "sonar-pro".
func NewPlugin(apiKey, model string, verbose bool) *Plugin {
	return &Plugin{client: NewClient(apiKey, model, verbose)}
}

func (p *Plugin) Name() string { return "perplexity" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		// Standard search — fast, cheap, great for pipelines
		"perplexity": p.search,

		// Pro search — deeper research, slower, more accurate
		"perplexity_pro": p.searchPro,

		// Time-filtered: perplexity_recent "query" "week"
		// recency: day, week, month, hour
		"perplexity_recent": p.searchRecent,

		// Domain-restricted: perplexity_domain "query" "site1.com,site2.com"
		"perplexity_domain": p.searchDomain,
	}
}

// search is the standard perplexity command.
// Usage: perplexity "what is the current state of Go generics?"
func (p *Plugin) search(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("perplexity requires a query")
	}
	return p.client.Search(ctx, query, SearchOptions{})
}

// searchPro uses sonar-pro for deeper, more accurate research.
// Usage: perplexity_pro "detailed analysis of Rust async ecosystem"
func (p *Plugin) searchPro(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("perplexity_pro requires a query")
	}
	return p.client.Search(ctx, query, SearchOptions{Model: ModelSonarPro})
}

// searchRecent restricts results to a time window.
// Usage: perplexity_recent "Go 1.24 features" "week"
// Recency values: hour, day, week, month
func (p *Plugin) searchRecent(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("perplexity_recent requires a query")
	}
	recency := plugin.Arg(args, 1)
	if recency == "" {
		recency = "week" // sensible default
	}
	validRecency := map[string]bool{"hour": true, "day": true, "week": true, "month": true}
	if !validRecency[recency] {
		return "", fmt.Errorf("perplexity_recent: recency must be hour/day/week/month, got %q", recency)
	}
	return p.client.Search(ctx, query, SearchOptions{RecencyFilter: recency})
}

// searchDomain restricts results to specific domains.
// Usage: perplexity_domain "Go best practices" "go.dev,pkg.go.dev"
func (p *Plugin) searchDomain(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("perplexity_domain requires a query")
	}
	domainsArg := plugin.Arg(args, 1)
	var domains []string
	if domainsArg != "" {
		for _, d := range strings.Split(domainsArg, ",") {
			if d = strings.TrimSpace(d); d != "" {
				domains = append(domains, d)
			}
		}
	}
	return p.client.Search(ctx, query, SearchOptions{DomainFilter: domains})
}
