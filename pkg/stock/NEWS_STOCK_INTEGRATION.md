# AgentScript: Adding NEWS + STOCK Commands

## Overview
- `news` — Search news articles or get top headlines (GNews API + SerpAPI fallback)
- `news_headlines` — Top headlines by category
- `stock` — Real-time stock quotes (Finnhub API + SerpAPI fallback)

## New Files
- `news.go` — Drop into project root
- `stock.go` — Drop into project root

## Environment Variables

```bash
# News (pick one or both)
export GNEWS_API_KEY="your-key"    # https://gnews.io (free: 100 req/day)
# OR falls back to SERPAPI_KEY (Google News engine)

# Stocks (pick one or both)
export FINNHUB_API_KEY="your-key"  # https://finnhub.io (free: 60 calls/min)
# OR falls back to SERPAPI_KEY (Google Finance engine)

# You already have this from job_search:
export SERPAPI_KEY="your-key"      # Works as fallback for both!
```

**Pro tip:** If you already have `SERPAPI_KEY` set, both news and stock work immediately with no extra keys. The dedicated APIs (GNews, Finnhub) just give better/faster results.

---

## 1. grammar.go — Add to Action regex

```go
Action string `@("news"|"news_headlines"|"stock"|"weather"|"job_search"|"search"|...)`
```

---

## 2. runtime.go — Add clients and cases

### 2a. Add fields to Runtime struct

```go
type Runtime struct {
    gemini    *GeminiClient
    google    *GoogleClient
    searcher  *JobSearcher
    weather   *WeatherClient
    news      *NewsClient     // <-- ADD
    stocks    *StockClient    // <-- ADD
    searchKey string
    verbose   bool
    procs     map[string]*ProcDef
}
```

### 2b. Initialize in NewRuntime

```go
func NewRuntime(gemini *GeminiClient, google *GoogleClient, verbose bool) *Runtime {
    serpKey := os.Getenv("SERPAPI_KEY")
    return &Runtime{
        gemini:   gemini,
        google:   google,
        searcher: NewJobSearcher(serpKey, verbose),
        weather:  NewWeatherClient(verbose),
        news:     NewNewsClient(os.Getenv("GNEWS_API_KEY"), serpKey, verbose),     // <-- ADD
        stocks:   NewStockClient(os.Getenv("FINNHUB_API_KEY"), serpKey, verbose),  // <-- ADD
        searchKey: serpKey,
        verbose:  verbose,
        procs:    make(map[string]*ProcDef),
    }
}
```

### 2c. Add cases in executeCommand switch

```go
    case "news":
        result, err = r.newsSearch(ctx, cmd.Arg, input)
    case "news_headlines":
        result, err = r.newsHeadlines(ctx, cmd.Arg, input)
    case "stock":
        result, err = r.stockQuote(ctx, cmd.Arg, input)
```

### 2d. Add handler methods

```go
// newsSearch searches for news articles
func (r *Runtime) newsSearch(ctx context.Context, query string, input string) (string, error) {
    q := query
    if q == "" && input != "" {
        q = strings.TrimSpace(input)
    }
    if q == "" {
        return "", fmt.Errorf("news requires a search query, e.g.: news \"golang\"")
    }

    r.log("NEWS: %q", q)
    articles, err := r.news.Search(ctx, q, 10)
    if err != nil {
        return "", fmt.Errorf("news search failed: %w", err)
    }

    return FormatNewsResults(articles, q), nil
}

// newsHeadlines fetches top headlines by category
func (r *Runtime) newsHeadlines(ctx context.Context, category string, input string) (string, error) {
    cat := category
    if cat == "" {
        cat = "technology" // default for devs
    }

    r.log("NEWS_HEADLINES: category=%q", cat)
    articles, err := r.news.TopHeadlines(ctx, cat, 10)
    if err != nil {
        return "", fmt.Errorf("headlines failed: %w", err)
    }

    return FormatNewsResults(articles, "Top "+cat+" headlines"), nil
}

// stockQuote fetches stock price(s)
func (r *Runtime) stockQuote(ctx context.Context, symbolsArg string, input string) (string, error) {
    arg := symbolsArg
    if arg == "" && input != "" {
        arg = strings.TrimSpace(input)
    }
    if arg == "" {
        return "", fmt.Errorf("stock requires symbol(s), e.g.: stock \"AAPL\" or stock \"AAPL,GOOGL,MSFT\"")
    }

    symbols := ParseStockSymbols(arg)
    r.log("STOCK: %v", symbols)

    if len(symbols) == 1 {
        // Single stock — get detailed quote + profile
        quote, err := r.stocks.GetQuote(ctx, symbols[0])
        if err != nil {
            return "", fmt.Errorf("stock quote failed: %w", err)
        }

        // Try to get profile too
        profile, _ := r.stocks.GetProfile(ctx, symbols[0])
        return FormatStockWithProfile(quote, profile), nil
    }

    // Multiple stocks — table view
    quotes, err := r.stocks.GetMultipleQuotes(ctx, symbols)
    if err != nil {
        return "", fmt.Errorf("stock quotes failed: %w", err)
    }

    return FormatStockQuotes(quotes), nil
}
```

---

## 3. DSL Usage

### News

```bash
# Search news
./agentscript -e 'news "golang"'
./agentscript -e 'news "artificial intelligence breakthroughs"'

# Top headlines by category
./agentscript -e 'news_headlines "technology"'
./agentscript -e 'news_headlines "business"'

# Categories: general, world, nation, business, technology, entertainment, sports, science, health

# News + summarize
./agentscript -e 'news "kubernetes security" -> summarize -> email "you@gmail.com"'

# Compare news across topics
./agentscript -e '
parallel {
  news "golang"
  news "rust programming"
  news "python AI"
}
-> merge
-> ask "compare the buzz around these languages. Which is trending most?"
'
```

### Stocks

```bash
# Single stock (detailed + company profile)
./agentscript -e 'stock "AAPL"'

# Multiple stocks (table view)
./agentscript -e 'stock "AAPL,GOOGL,MSFT,AMZN,NVDA"'

# Stock + analysis
./agentscript -e 'stock "NVDA" -> ask "based on this data, is NVIDIA overvalued?"'

# Portfolio check
./agentscript -e 'stock "AAPL,GOOGL,MSFT,AMZN,TSLA" -> ask "which stocks are up today? Rank by performance" -> email "you@gmail.com"'
```

### Combo Pipelines

```bash
# Morning market briefing
./agentscript -e '
parallel {
  news_headlines "business"
  stock "AAPL,GOOGL,MSFT,AMZN,NVDA,TSLA"
  weather "New York"
}
-> merge
-> ask "Create a morning market briefing:
  1. Market summary with stock performance
  2. Top business headlines
  3. Weather
  Format professionally."
-> email "you@gmail.com"
'

# Stock + related news
./agentscript -e '
parallel {
  stock "NVDA"
  news "NVIDIA earnings AI chips"
}
-> merge
-> ask "analyze NVIDIA stock in context of recent news. Bull or bear case?"
'

# Full daily briefing (save as daily-briefing.as)
parallel {
  weather "San Francisco"
  news_headlines "technology"
  news "golang jobs remote"
  stock "AAPL,GOOGL,MSFT,NVDA,META"
  job_search "golang contract" "remote"
}
-> merge
-> ask "
  Morning briefing with 5 sections:
  1. Weather & what to wear
  2. Tech headlines (top 5)
  3. Market snapshot (table)
  4. Golang news
  5. New job listings (top 5)
"
-> doc_create "Daily Briefing"
-> email "you@gmail.com"
```

---

## 4. translator.go — Teach Gemini

Add to your translator prompt:

```
news "query" - Search for news articles. Examples:
  news "golang"
  news "AI breakthroughs"
  news "stock market today"

news_headlines "category" - Get top headlines. Categories: general, world, nation, business, technology, entertainment, sports, science, health. Examples:
  news_headlines "technology"
  news_headlines "business"

stock "SYMBOL" or stock "SYM1,SYM2,SYM3" - Get real-time stock quotes. Use comma-separated symbols for multiple. Examples:
  stock "AAPL"
  stock "AAPL,GOOGL,MSFT"
  stock "NVDA,AMD,INTC"
```

---

## 5. Total command count: 38

Updated:
- Core (8): search, summarize, ask, analyze, save, read, list, merge
- Google Workspace (10): email, calendar, meet, drive_save, doc_create, sheet_create, sheet_append, task, contact_find, youtube_search
- Multimodal (5): image_generate, image_analyze, video_generate, video_analyze, images_to_video
- **Data (4): job_search, weather, news, stock** ← NEW CATEGORY
- Control (1): parallel
- Other (10+): translate, tts, places_search, youtube_upload, form_create, filter, sort, stdin, news_headlines, etc.
