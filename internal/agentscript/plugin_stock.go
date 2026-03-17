package agentscript

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
	"github.com/vinodhalaharvi/agentscript/pkg/stock"
)

// StockPlugin handles the "stock" command.
type StockPlugin struct {
	searchKey string
	cache     *cache.Cache
	verbose   bool
}

func NewStockPlugin(searchKey string, c *cache.Cache, verbose bool) *StockPlugin {
	return &StockPlugin{searchKey: searchKey, cache: c, verbose: verbose}
}

func (p *StockPlugin) Name() string { return "stock" }

func (p *StockPlugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"stock": p.fetch,
	}
}

func (p *StockPlugin) fetch(ctx context.Context, args []string, input string) (string, error) {
	symbols := firstArg(args)
	if symbols == "" {
		symbols = strings.TrimSpace(input)
	}
	if symbols == "" {
		return "", fmt.Errorf("stock requires symbol(s)")
	}
	client := stock.NewStockClient(os.Getenv("FINNHUB_API_KEY"), p.searchKey, p.verbose)
	return cache.CachedGet(p.cache, "stock", symbols, cache.CacheTTLStock, func() (string, error) {
		parsed := stock.ParseStockSymbols(symbols)
		var quotes []stock.StockQuote
		for _, sym := range parsed {
			q, err := client.GetQuote(ctx, sym)
			if err != nil {
				return "", err
			}
			quotes = append(quotes, *q)
		}
		return stock.FormatStockQuotes(quotes), nil
	})
}
