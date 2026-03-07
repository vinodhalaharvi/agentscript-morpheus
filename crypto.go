package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CryptoPrice represents a cryptocurrency price
type CryptoPrice struct {
	ID                string
	Symbol            string
	Name              string
	CurrentPrice      float64
	MarketCap         float64
	MarketCapRank     int
	TotalVolume       float64
	High24h           float64
	Low24h            float64
	PriceChange24h    float64
	PriceChangePct24h float64
	CirculatingSupply float64
	ATH               float64
	ATHChangePct      float64
	LastUpdated       string
}

// CryptoClient handles crypto API calls via CoinGecko (free, no key)
type CryptoClient struct {
	client  *http.Client
	verbose bool
}

// NewCryptoClient creates a new crypto client
func NewCryptoClient(verbose bool) *CryptoClient {
	return &CryptoClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (cc *CryptoClient) log(format string, args ...any) {
	if cc.verbose {
		fmt.Printf("[CRYPTO] "+format+"\n", args...)
	}
}

// well-known symbol -> coingecko id mapping
var cryptoIDMap = map[string]string{
	"BTC":   "bitcoin",
	"ETH":   "ethereum",
	"SOL":   "solana",
	"ADA":   "cardano",
	"DOT":   "polkadot",
	"DOGE":  "dogecoin",
	"SHIB":  "shiba-inu",
	"AVAX":  "avalanche-2",
	"MATIC": "matic-network",
	"LINK":  "chainlink",
	"UNI":   "uniswap",
	"ATOM":  "cosmos",
	"XRP":   "ripple",
	"LTC":   "litecoin",
	"BNB":   "binancecoin",
	"NEAR":  "near",
	"FTM":   "fantom",
	"ALGO":  "algorand",
	"XLM":   "stellar",
	"VET":   "vechain",
	"SAND":  "the-sandbox",
	"MANA":  "decentraland",
	"APE":   "apecoin",
	"OP":    "optimism",
	"ARB":   "arbitrum",
	"SUI":   "sui",
	"SEI":   "sei-network",
	"TIA":   "celestia",
	"JUP":   "jupiter-exchange-solana",
	"WIF":   "dogwifcoin",
	"PEPE":  "pepe",
}

// resolveSymbol converts a ticker symbol to CoinGecko ID
func resolveSymbol(symbol string) string {
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	if id, ok := cryptoIDMap[upper]; ok {
		return id
	}
	// If not in map, assume it's already a CoinGecko ID (lowercase)
	return strings.ToLower(symbol)
}

// GetPrices fetches prices for one or more cryptocurrencies
func (cc *CryptoClient) GetPrices(ctx context.Context, symbols []string) ([]CryptoPrice, error) {
	// Convert symbols to CoinGecko IDs
	var ids []string
	for _, s := range symbols {
		ids = append(ids, resolveSymbol(s))
	}

	idsParam := strings.Join(ids, ",")
	cc.log("Fetching prices for: %s", idsParam)

	params := url.Values{}
	params.Set("ids", idsParam)
	params.Set("vs_currency", "usd")
	params.Set("order", "market_cap_desc")
	params.Set("per_page", "100")
	params.Set("page", "1")
	params.Set("sparkline", "false")
	params.Set("price_change_percentage", "24h")

	apiURL := "https://api.coingecko.com/api/v3/coins/markets?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := cc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CoinGecko request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("CoinGecko rate limited. Free tier allows 10-30 calls/min. Wait and retry")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CoinGecko error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		ID                string  `json:"id"`
		Symbol            string  `json:"symbol"`
		Name              string  `json:"name"`
		CurrentPrice      float64 `json:"current_price"`
		MarketCap         float64 `json:"market_cap"`
		MarketCapRank     int     `json:"market_cap_rank"`
		TotalVolume       float64 `json:"total_volume"`
		High24h           float64 `json:"high_24h"`
		Low24h            float64 `json:"low_24h"`
		PriceChange24h    float64 `json:"price_change_24h"`
		PriceChangePct24h float64 `json:"price_change_percentage_24h"`
		CirculatingSupply float64 `json:"circulating_supply"`
		ATH               float64 `json:"ath"`
		ATHChangePct      float64 `json:"ath_change_percentage"`
		LastUpdated       string  `json:"last_updated"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse CoinGecko response: %w", err)
	}

	var prices []CryptoPrice
	for _, r := range raw {
		prices = append(prices, CryptoPrice{
			ID:                r.ID,
			Symbol:            strings.ToUpper(r.Symbol),
			Name:              r.Name,
			CurrentPrice:      r.CurrentPrice,
			MarketCap:         r.MarketCap,
			MarketCapRank:     r.MarketCapRank,
			TotalVolume:       r.TotalVolume,
			High24h:           r.High24h,
			Low24h:            r.Low24h,
			PriceChange24h:    r.PriceChange24h,
			PriceChangePct24h: r.PriceChangePct24h,
			CirculatingSupply: r.CirculatingSupply,
			ATH:               r.ATH,
			ATHChangePct:      r.ATHChangePct,
			LastUpdated:       r.LastUpdated,
		})
	}

	cc.log("Got %d crypto prices", len(prices))
	return prices, nil
}

// GetTopN fetches the top N cryptocurrencies by market cap
func (cc *CryptoClient) GetTopN(ctx context.Context, n int) ([]CryptoPrice, error) {
	if n <= 0 {
		n = 10
	}
	if n > 100 {
		n = 100
	}

	cc.log("Fetching top %d cryptos", n)

	params := url.Values{}
	params.Set("vs_currency", "usd")
	params.Set("order", "market_cap_desc")
	params.Set("per_page", fmt.Sprintf("%d", n))
	params.Set("page", "1")
	params.Set("sparkline", "false")
	params.Set("price_change_percentage", "24h")

	apiURL := "https://api.coingecko.com/api/v3/coins/markets?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := cc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CoinGecko error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		ID                string  `json:"id"`
		Symbol            string  `json:"symbol"`
		Name              string  `json:"name"`
		CurrentPrice      float64 `json:"current_price"`
		MarketCap         float64 `json:"market_cap"`
		MarketCapRank     int     `json:"market_cap_rank"`
		TotalVolume       float64 `json:"total_volume"`
		High24h           float64 `json:"high_24h"`
		Low24h            float64 `json:"low_24h"`
		PriceChange24h    float64 `json:"price_change_24h"`
		PriceChangePct24h float64 `json:"price_change_percentage_24h"`
		CirculatingSupply float64 `json:"circulating_supply"`
		ATH               float64 `json:"ath"`
		ATHChangePct      float64 `json:"ath_change_percentage"`
		LastUpdated       string  `json:"last_updated"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	var prices []CryptoPrice
	for _, r := range raw {
		prices = append(prices, CryptoPrice{
			ID:                r.ID,
			Symbol:            strings.ToUpper(r.Symbol),
			Name:              r.Name,
			CurrentPrice:      r.CurrentPrice,
			MarketCap:         r.MarketCap,
			MarketCapRank:     r.MarketCapRank,
			TotalVolume:       r.TotalVolume,
			High24h:           r.High24h,
			Low24h:            r.Low24h,
			PriceChange24h:    r.PriceChange24h,
			PriceChangePct24h: r.PriceChangePct24h,
			CirculatingSupply: r.CirculatingSupply,
			ATH:               r.ATH,
			ATHChangePct:      r.ATHChangePct,
			LastUpdated:       r.LastUpdated,
		})
	}

	return prices, nil
}

// ParseCryptoSymbols parses input into symbols
func ParseCryptoSymbols(input string) []string {
	input = strings.TrimSpace(input)

	// Handle "top 10", "top 20" etc
	if strings.HasPrefix(strings.ToLower(input), "top") {
		return nil // signal to use GetTopN
	}

	var symbols []string
	for _, part := range strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n'
	}) {
		sym := strings.TrimSpace(part)
		if sym != "" {
			symbols = append(symbols, sym)
		}
	}
	return symbols
}

// FormatCryptoPrice formats a single crypto price
func FormatCryptoPrice(p *CryptoPrice) string {
	arrow := "📈"
	if p.PriceChangePct24h < 0 {
		arrow = "📉"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s (%s) %s\n", p.Name, p.Symbol, arrow))

	// Format price based on magnitude
	if p.CurrentPrice >= 1 {
		sb.WriteString(fmt.Sprintf("**Price:** $%.2f", p.CurrentPrice))
	} else if p.CurrentPrice >= 0.01 {
		sb.WriteString(fmt.Sprintf("**Price:** $%.4f", p.CurrentPrice))
	} else {
		sb.WriteString(fmt.Sprintf("**Price:** $%.8f", p.CurrentPrice))
	}

	sign := "+"
	if p.PriceChangePct24h < 0 {
		sign = ""
	}
	sb.WriteString(fmt.Sprintf(" (%s%.2f%%)\n", sign, p.PriceChangePct24h))

	sb.WriteString(fmt.Sprintf("- **24h High/Low:** $%.2f / $%.2f\n", p.High24h, p.Low24h))
	sb.WriteString(fmt.Sprintf("- **Market Cap:** $%.0fM (#%d)\n", p.MarketCap/1_000_000, p.MarketCapRank))
	sb.WriteString(fmt.Sprintf("- **24h Volume:** $%.0fM\n", p.TotalVolume/1_000_000))
	sb.WriteString(fmt.Sprintf("- **ATH:** $%.2f (%.1f%% from ATH)\n", p.ATH, p.ATHChangePct))

	return sb.String()
}

// FormatCryptoPrices formats multiple crypto prices as a table
func FormatCryptoPrices(prices []CryptoPrice) string {
	if len(prices) == 0 {
		return "No crypto data found."
	}

	if len(prices) == 1 {
		return FormatCryptoPrice(&prices[0])
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Crypto Prices (%d coins)\n\n", len(prices)))
	sb.WriteString("| # | Coin | Symbol | Price | 24h Change | Market Cap | Volume 24h |\n")
	sb.WriteString("|---|------|--------|-------|------------|------------|------------|\n")

	for _, p := range prices {
		indicator := "🟢"
		if p.PriceChangePct24h < 0 {
			indicator = "🔴"
		} else if p.PriceChangePct24h == 0 {
			indicator = "⚪"
		}

		var priceStr string
		if p.CurrentPrice >= 1 {
			priceStr = fmt.Sprintf("$%.2f", p.CurrentPrice)
		} else if p.CurrentPrice >= 0.01 {
			priceStr = fmt.Sprintf("$%.4f", p.CurrentPrice)
		} else {
			priceStr = fmt.Sprintf("$%.6f", p.CurrentPrice)
		}

		sb.WriteString(fmt.Sprintf("| %d | %s %s | %s | %s | %+.2f%% | $%.0fM | $%.0fM |\n",
			p.MarketCapRank, indicator, p.Name, p.Symbol, priceStr,
			p.PriceChangePct24h, p.MarketCap/1_000_000, p.TotalVolume/1_000_000))
	}

	return sb.String()
}
