package crypto

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps CryptoClient as a Plugin.
type Plugin struct {
	client *CryptoClient
	cache  *cache.Cache
}

// NewPlugin creates a crypto plugin.
func NewPlugin(c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{
		client: NewCryptoClient(verbose),
		cache:  c,
	}
}

func (p *Plugin) Name() string { return "crypto" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"crypto": p.crypto,
	}
}

func (p *Plugin) crypto(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		query = "BTC,ETH,SOL"
	}
	return cache.CachedGet(p.cache, "crypto", query, cache.CacheTTLCrypto, func() (string, error) {
		symbols := ParseCryptoSymbols(query)
		if symbols == nil {
			n := 10
			fmt.Sscanf(strings.ToLower(query), "top %d", &n)
			prices, err := p.client.GetTopN(ctx, n)
			if err != nil {
				return "", err
			}
			return FormatCryptoPrices(prices), nil
		}
		prices, err := p.client.GetPrices(ctx, symbols)
		if err != nil {
			return "", err
		}
		return FormatCryptoPrices(prices), nil
	})
}
