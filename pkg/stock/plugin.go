package stock

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps StockClient as a Plugin.
type Plugin struct {
	searchKey string
	cache     *cache.Cache
	verbose   bool
}

// NewPlugin creates a stock plugin.
func NewPlugin(searchKey string, c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{searchKey: searchKey, cache: c, verbose: verbose}
}

func (p *Plugin) Name() string { return "stock" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"stock": p.stock,
	}
}

func (p *Plugin) stock(ctx context.Context, args []string, input string) (string, error) {
	symbols := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if symbols == "" {
		return "", fmt.Errorf("stock requires symbol(s)")
	}
	client := NewStockClient(os.Getenv("FINNHUB_API_KEY"), p.searchKey, p.verbose)
	return cache.CachedGet(p.cache, "stock", symbols, cache.CacheTTLStock, func() (string, error) {
		parsed := ParseStockSymbols(symbols)
		var quotes []StockQuote
		for _, sym := range parsed {
			q, err := client.GetQuote(ctx, sym)
			if err != nil {
				return "", err
			}
			quotes = append(quotes, *q)
		}
		return FormatStockQuotes(quotes), nil
	})
}
