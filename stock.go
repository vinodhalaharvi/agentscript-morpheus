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

// StockQuote represents a stock price quote
type StockQuote struct {
	Symbol        string
	CurrentPrice  float64
	Change        float64
	ChangePercent float64
	High          float64
	Low           float64
	Open          float64
	PrevClose     float64
	Timestamp     int64
}

// CompanyProfile represents basic company info
type CompanyProfile struct {
	Name      string
	Ticker    string
	Exchange  string
	Industry  string
	MarketCap float64
	WebURL    string
	Logo      string
	Country   string
}

// StockClient handles stock API calls
type StockClient struct {
	finnhubKey string // Finnhub API key
	serpKey    string // SerpAPI key (fallback)
	client     *http.Client
	verbose    bool
}

// NewStockClient creates a new stock client
func NewStockClient(finnhubKey, serpKey string, verbose bool) *StockClient {
	return &StockClient{
		finnhubKey: finnhubKey,
		serpKey:    serpKey,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (sc *StockClient) log(format string, args ...any) {
	if sc.verbose {
		fmt.Printf("[STOCK] "+format+"\n", args...)
	}
}

// GetQuote fetches a stock quote
func (sc *StockClient) GetQuote(ctx context.Context, symbol string) (*StockQuote, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	// Try Finnhub first
	if sc.finnhubKey != "" {
		quote, err := sc.quoteFinnhub(ctx, symbol)
		if err == nil {
			return quote, nil
		}
		sc.log("Finnhub failed, falling back: %v", err)
	}

	// Fallback to SerpAPI Google Finance
	if sc.serpKey != "" {
		return sc.quoteSerpAPI(ctx, symbol)
	}

	return nil, fmt.Errorf("no stock API key available. Set FINNHUB_API_KEY or SERPAPI_KEY")
}

// GetProfile fetches company profile
func (sc *StockClient) GetProfile(ctx context.Context, symbol string) (*CompanyProfile, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	if sc.finnhubKey != "" {
		return sc.profileFinnhub(ctx, symbol)
	}

	return nil, fmt.Errorf("FINNHUB_API_KEY required for company profile")
}

// GetMultipleQuotes fetches quotes for multiple symbols
func (sc *StockClient) GetMultipleQuotes(ctx context.Context, symbols []string) ([]StockQuote, error) {
	var quotes []StockQuote

	for _, sym := range symbols {
		quote, err := sc.GetQuote(ctx, sym)
		if err != nil {
			sc.log("Failed to get quote for %s: %v", sym, err)
			continue
		}
		quotes = append(quotes, *quote)

		// Rate limit between calls
		time.Sleep(200 * time.Millisecond)
	}

	return quotes, nil
}

// quoteFinnhub gets a quote from Finnhub
func (sc *StockClient) quoteFinnhub(ctx context.Context, symbol string) (*StockQuote, error) {
	apiURL := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s&token=%s",
		url.QueryEscape(symbol), sc.finnhubKey)

	sc.log("Finnhub quote: %s", symbol)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := sc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Finnhub error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		C  float64 `json:"c"`  // Current price
		D  float64 `json:"d"`  // Change
		Dp float64 `json:"dp"` // Percent change
		H  float64 `json:"h"`  // High
		L  float64 `json:"l"`  // Low
		O  float64 `json:"o"`  // Open
		Pc float64 `json:"pc"` // Previous close
		T  int64   `json:"t"`  // Timestamp
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Finnhub response: %w", err)
	}

	if raw.C == 0 {
		return nil, fmt.Errorf("no data for symbol %s (price is 0)", symbol)
	}

	return &StockQuote{
		Symbol:        symbol,
		CurrentPrice:  raw.C,
		Change:        raw.D,
		ChangePercent: raw.Dp,
		High:          raw.H,
		Low:           raw.L,
		Open:          raw.O,
		PrevClose:     raw.Pc,
		Timestamp:     raw.T,
	}, nil
}

// profileFinnhub gets company profile from Finnhub
func (sc *StockClient) profileFinnhub(ctx context.Context, symbol string) (*CompanyProfile, error) {
	apiURL := fmt.Sprintf("https://finnhub.io/api/v1/stock/profile2?symbol=%s&token=%s",
		url.QueryEscape(symbol), sc.finnhubKey)

	sc.log("Finnhub profile: %s", symbol)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := sc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Name            string  `json:"name"`
		Ticker          string  `json:"ticker"`
		Exchange        string  `json:"exchange"`
		FinnhubIndustry string  `json:"finnhubIndustry"`
		MarketCap       float64 `json:"marketCapitalization"`
		WebURL          string  `json:"weburl"`
		Logo            string  `json:"logo"`
		Country         string  `json:"country"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	return &CompanyProfile{
		Name:      raw.Name,
		Ticker:    raw.Ticker,
		Exchange:  raw.Exchange,
		Industry:  raw.FinnhubIndustry,
		MarketCap: raw.MarketCap,
		WebURL:    raw.WebURL,
		Logo:      raw.Logo,
		Country:   raw.Country,
	}, nil
}

// quoteSerpAPI gets a quote from SerpAPI Google Finance
func (sc *StockClient) quoteSerpAPI(ctx context.Context, symbol string) (*StockQuote, error) {
	params := url.Values{}
	params.Set("engine", "google_finance")
	params.Set("q", symbol)
	params.Set("api_key", sc.serpKey)

	apiURL := "https://serpapi.com/search.json?" + params.Encode()
	sc.log("SerpAPI finance: %s", symbol)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := sc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("SerpAPI error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	quote := &StockQuote{Symbol: symbol}

	// Parse summary section
	if summary, ok := result["summary"].(map[string]any); ok {
		if price, ok := summary["price"].(float64); ok {
			quote.CurrentPrice = price
		}
		if change, ok := summary["price_change"].(float64); ok {
			quote.Change = change
		}
		if pct, ok := summary["price_change_percentage"].(float64); ok {
			quote.ChangePercent = pct
		}
	}

	// Parse market trends for high/low
	if graph, ok := result["graph"].([]any); ok && len(graph) > 0 {
		var high, low float64
		for _, point := range graph {
			if p, ok := point.(map[string]any); ok {
				if price, ok := p["price"].(float64); ok {
					if high == 0 || price > high {
						high = price
					}
					if low == 0 || price < low {
						low = price
					}
				}
			}
		}
		quote.High = high
		quote.Low = low
	}

	return quote, nil
}

// ParseStockSymbols parses a string into stock symbols
// Supports: "AAPL", "AAPL GOOGL MSFT", "AAPL,GOOGL,MSFT"
func ParseStockSymbols(input string) []string {
	input = strings.TrimSpace(input)

	// Split by comma, space, or both
	var symbols []string
	for _, part := range strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	}) {
		sym := strings.ToUpper(strings.TrimSpace(part))
		if sym != "" && len(sym) <= 10 { // basic validation
			symbols = append(symbols, sym)
		}
	}

	return symbols
}

// FormatStockQuote formats a single quote
func FormatStockQuote(q *StockQuote) string {
	arrow := "📈"
	if q.Change < 0 {
		arrow = "📉"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s %s $%.2f", q.Symbol, arrow, q.CurrentPrice))
	if q.Change != 0 {
		sign := "+"
		if q.Change < 0 {
			sign = ""
		}
		sb.WriteString(fmt.Sprintf(" (%s%.2f / %s%.2f%%)", sign, q.Change, sign, q.ChangePercent))
	}
	sb.WriteString("\n")

	if q.Open > 0 {
		sb.WriteString(fmt.Sprintf("- **Open:** $%.2f\n", q.Open))
	}
	if q.High > 0 {
		sb.WriteString(fmt.Sprintf("- **High:** $%.2f\n", q.High))
	}
	if q.Low > 0 {
		sb.WriteString(fmt.Sprintf("- **Low:** $%.2f\n", q.Low))
	}
	if q.PrevClose > 0 {
		sb.WriteString(fmt.Sprintf("- **Prev Close:** $%.2f\n", q.PrevClose))
	}

	return sb.String()
}

// FormatStockQuotes formats multiple quotes as a table
func FormatStockQuotes(quotes []StockQuote) string {
	if len(quotes) == 0 {
		return "No stock data found."
	}

	if len(quotes) == 1 {
		return FormatStockQuote(&quotes[0])
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Stock Quotes (%d symbols)\n\n", len(quotes)))
	sb.WriteString("| Symbol | Price | Change | Change% | Open | High | Low | Prev Close |\n")
	sb.WriteString("|--------|-------|--------|---------|------|------|-----|------------|\n")

	for _, q := range quotes {
		arrow := "🟢"
		if q.Change < 0 {
			arrow = "🔴"
		} else if q.Change == 0 {
			arrow = "⚪"
		}

		sb.WriteString(fmt.Sprintf("| %s %s | $%.2f | %+.2f | %+.2f%% | $%.2f | $%.2f | $%.2f | $%.2f |\n",
			arrow, q.Symbol, q.CurrentPrice, q.Change, q.ChangePercent,
			q.Open, q.High, q.Low, q.PrevClose))
	}

	return sb.String()
}

// FormatStockWithProfile formats a quote with company info
func FormatStockWithProfile(q *StockQuote, p *CompanyProfile) string {
	var sb strings.Builder

	if p != nil {
		sb.WriteString(fmt.Sprintf("# %s (%s)\n", p.Name, q.Symbol))
		sb.WriteString(fmt.Sprintf("**Exchange:** %s | **Industry:** %s | **Country:** %s\n",
			p.Exchange, p.Industry, p.Country))
		if p.MarketCap > 0 {
			sb.WriteString(fmt.Sprintf("**Market Cap:** $%.2fB\n", p.MarketCap/1000))
		}
		if p.WebURL != "" {
			sb.WriteString(fmt.Sprintf("**Website:** %s\n", p.WebURL))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(FormatStockQuote(q))
	return sb.String()
}
