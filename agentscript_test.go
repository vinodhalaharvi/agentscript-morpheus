package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers
// ============================================================================

func testContext() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), 30*time.Second)
	return ctx
}

func skipIfNoKey(t *testing.T, envVar string) {
	if os.Getenv(envVar) == "" {
		t.Skipf("Skipping: %s not set", envVar)
	}
}

// ============================================================================
// Core Parsing Tests (no API keys needed)
// ============================================================================

func TestParseCondition(t *testing.T) {
	tests := []struct {
		input    string
		operator string
		left     string
		right    string
	}{
		{"rain > 50", ">", "rain", "50"},
		{"price >= 100", ">=", "price", "100"},
		{"change < 0", "<", "change", "0"},
		{"count == 10", "==", "count", "10"},
		{"status != error", "!=", "status", "error"},
		{`contains "golang"`, "contains", "_input", "golang"},
		{`not_contains "error"`, "not_contains", "_input", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cond, err := ParseCondition(tt.input)
			if err != nil {
				t.Fatalf("ParseCondition(%q) error: %v", tt.input, err)
			}
			if cond.Operator != tt.operator {
				t.Errorf("operator: got %q, want %q", cond.Operator, tt.operator)
			}
			if cond.Left != tt.left {
				t.Errorf("left: got %q, want %q", cond.Left, tt.left)
			}
			if cond.Right != tt.right {
				t.Errorf("right: got %q, want %q", cond.Right, tt.right)
			}
		})
	}
}

func TestEvaluateCondition(t *testing.T) {
	tests := []struct {
		condition string
		input     string
		expected  bool
	}{
		{`contains "golang"`, "I love golang programming", true},
		{`contains "rust"`, "I love golang programming", false},
		{`not_contains "error"`, "everything is fine", true},
		{`not_contains "fine"`, "everything is fine", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result, err := EvaluateConditionString(tt.condition, tt.input, nil)
			if err != nil {
				t.Fatalf("Evaluate error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractFirstNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Temperature: 72°F", "72"},
		{"Rain: 85%", "85"},
		{"Price: $150.50", "150.50"},
		{"-5.2% change", "-5.2"},
		{"no numbers here", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractFirstNumber(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Symbol/Input Parsing Tests
// ============================================================================

func TestParseStockSymbols(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"AAPL", []string{"AAPL"}},
		{"AAPL,GOOGL,MSFT", []string{"AAPL", "GOOGL", "MSFT"}},
		{"aapl googl msft", []string{"AAPL", "GOOGL", "MSFT"}},
		{"  NVDA , TSLA  ", []string{"NVDA", "TSLA"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseStockSymbols(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d symbols, want %d: %v", len(result), len(tt.expected), result)
			}
			for i, s := range result {
				if s != tt.expected[i] {
					t.Errorf("symbol[%d]: got %q, want %q", i, s, tt.expected[i])
				}
			}
		})
	}
}

func TestParseCryptoSymbols(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
		isTop    bool
	}{
		{"BTC,ETH,SOL", []string{"BTC", "ETH", "SOL"}, false},
		{"btc eth", []string{"btc", "eth"}, false},
		{"top 10", nil, true},
		{"top 20", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseCryptoSymbols(tt.input)
			if tt.isTop {
				if result != nil {
					t.Errorf("expected nil for top query, got %v", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d, want %d", len(result), len(tt.expected))
			}
		})
	}
}

func TestParseJobSearchArgs(t *testing.T) {
	tests := []struct {
		args     []string
		query    string
		location string
		empType  string
	}{
		{[]string{"golang contract"}, "golang contract", "", "CONTRACTOR"},
		{[]string{"go developer", "remote"}, "go developer", "remote", ""},
		{[]string{"go dev", "NYC", "CONTRACTOR"}, "go dev", "NYC", "CONTRACTOR"},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "|"), func(t *testing.T) {
			config := ParseJobSearchArgs(tt.args...)
			if config.Query != tt.query {
				t.Errorf("query: got %q, want %q", config.Query, tt.query)
			}
			if config.Location != tt.location {
				t.Errorf("location: got %q, want %q", config.Location, tt.location)
			}
			if config.EmploymentType != tt.empType {
				t.Errorf("empType: got %q, want %q", config.EmploymentType, tt.empType)
			}
		})
	}
}

func TestParseRedditArgs(t *testing.T) {
	tests := []struct {
		args  []string
		isSub bool
		query string
		sort  string
	}{
		{[]string{"r/golang"}, true, "r/golang", ""},
		{[]string{"r/golang", "new"}, true, "r/golang", "new"},
		{[]string{"kubernetes best practices"}, false, "kubernetes best practices", ""},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "|"), func(t *testing.T) {
			isSub, query, sort := ParseRedditArgs(tt.args...)
			if isSub != tt.isSub {
				t.Errorf("isSub: got %v, want %v", isSub, tt.isSub)
			}
			if query != tt.query {
				t.Errorf("query: got %q, want %q", query, tt.query)
			}
			if sort != tt.sort && tt.sort != "" {
				t.Errorf("sort: got %q, want %q", sort, tt.sort)
			}
		})
	}
}

func TestResolveFeedURL(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"hn", "ycombinator"},
		{"golang", "go.dev"},
		{"techcrunch", "techcrunch.com"},
		{"https://myblog.com/feed", "myblog.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := resolveFeedURL(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("got %q, want it to contain %q", result, tt.contains)
			}
		})
	}
}

func TestNormalizeWhatsAppNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"whatsapp:+14155238886", "whatsapp:+14155238886"},
		{"+14155238886", "whatsapp:+14155238886"},
		{"4155238886", "whatsapp:+14155238886"}, // 10 digits = US
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeWhatsAppNumber(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Loop/ForEach Tests
// ============================================================================

func TestParseLoopItems(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		separator string
		minCount  int
	}{
		{
			"lines",
			"line1\nline2\nline3",
			"line",
			3,
		},
		{
			"csv",
			"a, b, c, d",
			"csv",
			4,
		},
		{
			"sections",
			"section1\n---\nsection2\n---\nsection3",
			"section",
			3,
		},
		{
			"skip_headers",
			"# Header\n\ndata1\ndata2\n---\ndata3",
			"line",
			3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := ParseLoopItems(tt.input, tt.separator)
			if len(items) < tt.minCount {
				t.Errorf("got %d items, want at least %d: %v", len(items), tt.minCount, items)
			}
		})
	}
}

func TestExecuteForEach(t *testing.T) {
	ctx := testContext()
	items := []string{"item1", "item2", "item3"}
	config := LoopConfig{Delay: 0}

	results, err := ExecuteForEach(ctx, items, config, false,
		func(ctx context.Context, item string, index int) (string, error) {
			return "processed: " + item, nil
		},
	)

	if err != nil {
		t.Fatalf("ExecuteForEach error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("item %d error: %v", r.Index, r.Error)
		}
		if !strings.HasPrefix(r.Output, "processed:") {
			t.Errorf("item %d output: got %q", r.Index, r.Output)
		}
	}
}

// ============================================================================
// Retry Tests
// ============================================================================

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		errMsg    string
		retryable bool
	}{
		{"status 429: rate limit", true},
		{"resource exhausted", true},
		{"too many requests", true},
		{"connection refused", true},
		{"status 503", true},
		{"invalid input", false},
		{"not found", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf(tt.errMsg)
			}
			result := isRetryableError(err)
			if result != tt.retryable {
				t.Errorf("got %v, want %v", result, tt.retryable)
			}
		})
	}
}

func TestWithRetrySuccess(t *testing.T) {
	ctx := testContext()
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		JitterPct:    0,
	}

	attempts := 0
	result, err := WithRetryString(ctx, config, "test", false, func() (string, error) {
		attempts++
		if attempts < 3 {
			return "", fmt.Errorf("status 429: rate limit")
		}
		return "success", nil
	})

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result != "success" {
		t.Errorf("got %q, want 'success'", result)
	}
	if attempts != 3 {
		t.Errorf("got %d attempts, want 3", attempts)
	}
}

func TestWithRetryNonRetryable(t *testing.T) {
	ctx := testContext()
	config := DefaultRetryConfig()
	config.InitialDelay = 10 * time.Millisecond

	attempts := 0
	_, err := WithRetryString(ctx, config, "test", false, func() (string, error) {
		attempts++
		return "", fmt.Errorf("invalid input: bad request")
	})

	if err == nil {
		t.Fatal("expected error for non-retryable")
	}
	if attempts != 1 {
		t.Errorf("non-retryable should only try once, got %d attempts", attempts)
	}
}

// ============================================================================
// Cache Tests
// ============================================================================

func TestCache(t *testing.T) {
	// Use temp directory
	tmpDir := t.TempDir()
	os.Setenv("AGENTSCRIPT_CACHE_DIR", tmpDir)
	defer os.Unsetenv("AGENTSCRIPT_CACHE_DIR")

	cache := NewCache(false)

	// Test miss
	_, ok := cache.Get("test", "key1")
	if ok {
		t.Error("expected cache miss")
	}

	// Test set + hit
	cache.Set("test", "key1", "hello world", 60)
	data, ok := cache.Get("test", "key1")
	if !ok {
		t.Error("expected cache hit")
	}
	if data != "hello world" {
		t.Errorf("got %q, want 'hello world'", data)
	}

	// Test expiry
	cache.Set("test", "key2", "expired", 1) // 1 second TTL
	time.Sleep(1100 * time.Millisecond)
	_, ok = cache.Get("test", "key2")
	if ok {
		t.Error("expected cache miss after expiry")
	}

	// Test invalidate
	cache.Set("test", "key3", "data", 60)
	cache.Invalidate("test", "key3")
	_, ok = cache.Get("test", "key3")
	if ok {
		t.Error("expected cache miss after invalidate")
	}

	// Test CachedGet
	calls := 0
	result, err := CachedGet(cache, "test", "cached1", 60, func() (string, error) {
		calls++
		return "fetched", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "fetched" {
		t.Errorf("got %q, want 'fetched'", result)
	}

	// Second call should hit cache
	result2, _ := CachedGet(cache, "test", "cached1", 60, func() (string, error) {
		calls++
		return "fetched-again", nil
	})
	if result2 != "fetched" {
		t.Errorf("cache should return original, got %q", result2)
	}
	if calls != 1 {
		t.Errorf("expected 1 fetch call, got %d", calls)
	}
}

// ============================================================================
// Format Tests
// ============================================================================

func TestFormatStockQuotes(t *testing.T) {
	quotes := []StockQuote{
		{Symbol: "AAPL", CurrentPrice: 185.50, Change: 2.30, ChangePercent: 1.25},
		{Symbol: "GOOGL", CurrentPrice: 140.20, Change: -1.10, ChangePercent: -0.78},
	}

	result := FormatStockQuotes(quotes)
	if !strings.Contains(result, "AAPL") {
		t.Error("missing AAPL in output")
	}
	if !strings.Contains(result, "GOOGL") {
		t.Error("missing GOOGL in output")
	}
	if !strings.Contains(result, "185.50") {
		t.Error("missing price in output")
	}
}

func TestFormatCryptoPrices(t *testing.T) {
	prices := []CryptoPrice{
		{Symbol: "BTC", Name: "Bitcoin", CurrentPrice: 95000, PriceChangePct24h: 2.5, MarketCapRank: 1, MarketCap: 1800000000000},
		{Symbol: "ETH", Name: "Ethereum", CurrentPrice: 3200, PriceChangePct24h: -1.2, MarketCapRank: 2, MarketCap: 380000000000},
	}

	result := FormatCryptoPrices(prices)
	if !strings.Contains(result, "BTC") {
		t.Error("missing BTC")
	}
	if !strings.Contains(result, "ETH") {
		t.Error("missing ETH")
	}
}

func TestFormatWeather(t *testing.T) {
	w := &WeatherData{
		Location: "New York, NY",
		Current: CurrentWeather{
			Temperature: 72,
			FeelsLike:   70,
			Humidity:    55,
			WindSpeed:   8,
			Condition:   "Partly cloudy",
		},
	}

	result := FormatWeather(w)
	if !strings.Contains(result, "New York") {
		t.Error("missing location")
	}
	if !strings.Contains(result, "72") {
		t.Error("missing temperature")
	}
	if !strings.Contains(result, "Partly cloudy") {
		t.Error("missing condition")
	}
}

func TestFormatNewsResults(t *testing.T) {
	articles := []NewsArticle{
		{Title: "Go 1.23 Released", Source: "Go Blog", Description: "New features"},
		{Title: "AI News", Source: "TechCrunch", Description: "Latest updates"},
	}

	result := FormatNewsResults(articles, "golang")
	if !strings.Contains(result, "Go 1.23") {
		t.Error("missing article title")
	}
	if !strings.Contains(result, "golang") {
		t.Error("missing query in header")
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<p>Hello <b>world</b></p>", "Hello world"},
		{"no tags here", "no tags here"},
		{"<a href='url'>link</a>", "link"},
	}

	for _, tt := range tests {
		result := stripHTML(tt.input)
		if result != tt.expected {
			t.Errorf("stripHTML(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestWMOCodeToCondition(t *testing.T) {
	tests := []struct {
		code     int
		contains string
	}{
		{0, "Clear"},
		{3, "Overcast"},
		{61, "rain"},
		{95, "Thunderstorm"},
		{75, "snow"},
	}

	for _, tt := range tests {
		result := wmoCodeToCondition(tt.code)
		if !strings.Contains(strings.ToLower(result), strings.ToLower(tt.contains)) {
			t.Errorf("wmoCodeToCondition(%d) = %q, want to contain %q", tt.code, result, tt.contains)
		}
	}
}

// ============================================================================
// Integration Tests (require API keys, skipped if not set)
// ============================================================================

func TestWeatherIntegration(t *testing.T) {
	// No key needed — Open-Meteo is free
	ctx := testContext()
	client := NewWeatherClient(false)

	data, err := client.GetWeather(ctx, "New York")
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") || strings.Contains(err.Error(), "connection") {
			t.Skipf("Skipping: network blocked in sandbox: %v", err)
		}
		t.Fatalf("Weather error: %v", err)
	}

	if data.Location == "" {
		t.Error("empty location")
	}
	if data.Current.Temperature == 0 && data.Current.Humidity == 0 {
		t.Error("no current weather data")
	}
	if len(data.DailyNext) == 0 {
		t.Error("no daily forecast")
	}
}

func TestCryptoIntegration(t *testing.T) {
	// No key needed — CoinGecko free tier
	ctx := testContext()
	client := NewCryptoClient(false)

	prices, err := client.GetPrices(ctx, []string{"BTC", "ETH"})
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") || strings.Contains(err.Error(), "connection") {
			t.Skipf("Skipping: network blocked in sandbox: %v", err)
		}
		t.Fatalf("Crypto error: %v", err)
	}

	if len(prices) == 0 {
		t.Fatal("no crypto prices returned")
	}

	for _, p := range prices {
		if p.CurrentPrice == 0 {
			t.Errorf("%s price is 0", p.Symbol)
		}
	}
}

func TestRedditIntegration(t *testing.T) {
	// No key needed — public JSON
	ctx := testContext()
	client := NewRedditClient(false)

	posts, err := client.SearchSubreddit(ctx, "golang", "hot", 5)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") || strings.Contains(err.Error(), "connection") {
			t.Skipf("Skipping: network blocked in sandbox: %v", err)
		}
		t.Fatalf("Reddit error: %v", err)
	}

	if len(posts) == 0 {
		t.Fatal("no Reddit posts returned")
	}

	for _, p := range posts {
		if p.Title == "" {
			t.Error("empty post title")
		}
		if p.Subreddit == "" {
			t.Error("empty subreddit")
		}
	}
}

func TestRSSIntegration(t *testing.T) {
	// No key needed
	ctx := testContext()
	client := NewRSSClient(false)

	items, title, err := client.FetchFeed(ctx, "hn", 5)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") || strings.Contains(err.Error(), "connection") {
			t.Skipf("Skipping: network blocked in sandbox: %v", err)
		}
		t.Fatalf("RSS error: %v", err)
	}

	if title == "" {
		t.Error("empty feed title")
	}
	if len(items) == 0 {
		t.Fatal("no RSS items returned")
	}
}

func TestJobSearchIntegration(t *testing.T) {
	skipIfNoKey(t, "SERPAPI_KEY")
	ctx := testContext()
	client := NewJobSearcher(os.Getenv("SERPAPI_KEY"), false)

	config := JobSearchConfig{
		Query:    "golang developer",
		NumPages: 1,
	}

	jobs, err := client.Search(ctx, config)
	if err != nil {
		t.Fatalf("Job search error: %v", err)
	}

	if len(jobs) == 0 {
		t.Fatal("no jobs returned")
	}
}

func TestStockIntegration(t *testing.T) {
	skipIfNoKey(t, "FINNHUB_API_KEY")
	ctx := testContext()
	client := NewStockClient(os.Getenv("FINNHUB_API_KEY"), "", false)

	quote, err := client.GetQuote(ctx, "AAPL")
	if err != nil {
		t.Fatalf("Stock error: %v", err)
	}

	if quote.CurrentPrice == 0 {
		t.Error("AAPL price is 0")
	}
}

func TestNewsIntegration(t *testing.T) {
	skipIfNoKey(t, "GNEWS_API_KEY")
	ctx := testContext()
	client := NewNewsClient(os.Getenv("GNEWS_API_KEY"), "", false)

	articles, err := client.Search(ctx, "golang", 5)
	if err != nil {
		t.Fatalf("News error: %v", err)
	}

	if len(articles) == 0 {
		t.Fatal("no news articles returned")
	}
}

func TestTwitterSearchIntegration(t *testing.T) {
	skipIfNoKey(t, "TWITTER_BEARER_TOKEN")
	ctx := testContext()
	client := NewTwitterClient(false)

	tweets, err := client.SearchRecent(ctx, "golang", 5)
	if err != nil {
		t.Fatalf("Twitter search error: %v", err)
	}

	if len(tweets) == 0 {
		t.Fatal("no tweets returned")
	}
}

// ============================================================================
// Emoji Style Tests
// ============================================================================

func TestExtractEmojis(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"😀😎🔥", []string{"😀", "😎", "🔥"}},
		{"hello 🚀 world 🎉", []string{"🚀", "🎉"}},
		{"👍👎🎯💡", []string{"👍", "👎", "🎯", "💡"}},
		{"no emojis here", nil},
		{"🐶🐱🐭🐹", []string{"🐶", "🐱", "🐭", "🐹"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ExtractEmojis(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("ExtractEmojis(%q) = %v (len %d), want %v (len %d)",
					tt.input, result, len(result), tt.expected, len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("ExtractEmojis(%q)[%d] = %q, want %q",
						tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestDetectEmojiSources(t *testing.T) {
	// Unicode emojis
	sources, err := DetectEmojiSources("😀😎🔥")
	if err != nil {
		t.Fatalf("DetectEmojiSources failed: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("Expected 3 sources, got %d", len(sources))
	}
	if sources[0].IsCustom {
		t.Error("Expected unicode emoji, got custom")
	}
	if sources[0].Description != "grinning face" {
		t.Errorf("Expected 'grinning face', got %q", sources[0].Description)
	}

	// No emojis should error
	_, err = DetectEmojiSources("no emojis here")
	if err == nil {
		t.Error("Expected error for non-emoji input")
	}
}

func TestIsEmojiInput(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"😀😎🔥", true},
		{"hello world", false},
		{"😀 😎 🔥", true},
		{"wearing a suit", false},
		{"🔥", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isEmojiInput(tt.input)
			if result != tt.expected {
				t.Errorf("isEmojiInput(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseEmojiStyleArgs(t *testing.T) {
	config := ParseEmojiStyleArgs("😀😎", "wearing suits", "hf")
	if config.Style != "wearing suits" {
		t.Errorf("Expected style 'wearing suits', got %q", config.Style)
	}
	if config.Engine != "hf" {
		t.Errorf("Expected engine 'hf', got %q", config.Engine)
	}

	// Auto engine
	config2 := ParseEmojiStyleArgs("😀", "pixel art", "")
	if config2.Engine != "auto" {
		t.Errorf("Expected engine 'auto', got %q", config2.Engine)
	}
}

func TestBuildPrompt(t *testing.T) {
	client := NewEmojiStyleClient(false)
	source := EmojiSource{
		Emoji:       "😎",
		Name:        "smiling_face_with_sunglasses",
		Description: "smiling face with sunglasses",
	}
	prompt := client.buildPrompt(source, "wearing a tuxedo with red bow tie")
	if !strings.Contains(prompt, "smiling face with sunglasses") {
		t.Error("Prompt should contain emoji description")
	}
	if !strings.Contains(prompt, "tuxedo") {
		t.Error("Prompt should contain style")
	}
	if !strings.Contains(prompt, "512x512") {
		t.Error("Prompt should specify sticker size")
	}
}
