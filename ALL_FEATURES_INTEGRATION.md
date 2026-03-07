# AgentScript: Batch Update — 6 New Features

## New Files (drop into project root)
1. `crypto.go` — Crypto prices via CoinGecko (free, **no API key**)
2. `reddit.go` — Reddit search/browse (free, **no API key**)
3. `rss.go` — RSS/Atom feed reader (free, **no API key**)
4. `notify.go` — Slack/Discord/Telegram webhooks
5. `cache.go` — File-based cache to avoid rate limits
6. `conditional.go` — If/else conditional logic

## Environment Variables

```bash
# Already have (from previous commands):
export SERPAPI_KEY="..."
export GEMINI_API_KEY="..."

# NEW - Notifications (set whichever you use):
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..."
export TELEGRAM_BOT_TOKEN="123456:ABC-DEF..."
export TELEGRAM_CHAT_ID="123456789"

# NEW - Cache (optional, defaults to ~/.agentscript/cache):
export AGENTSCRIPT_CACHE_DIR="/path/to/cache"

# crypto, reddit, rss — NO KEYS NEEDED!
```

---

## 1. grammar.go — Add all new actions

```go
Action string `@("crypto"|"reddit"|"rss"|"notify"|"if"|"news"|"news_headlines"|"stock"|"weather"|"job_search"|"search"|...)`
```

### If/Else Grammar Extension

Add to your Participle grammar:

```go
// IfBlock represents a conditional
type IfBlock struct {
    Condition string     `"if" @String`
    Then      []*Command `"{" @@* "}"`
    Else      []*Command `("else" "{" @@* "}")?`
}

// Update Statement to include IfBlock
type Statement struct {
    If       *IfBlock   `  @@`
    Parallel *Parallel  `| @@`
    Command  *Command   `| @@`
    Pipe     *Statement `("->" @@)?`
}
```

---

## 2. runtime.go — Add all new clients and cases

### 2a. Add fields to Runtime struct

```go
type Runtime struct {
    gemini   *GeminiClient
    google   *GoogleClient
    searcher *JobSearcher
    weather  *WeatherClient
    news     *NewsClient
    stocks   *StockClient
    crypto   *CryptoClient    // <-- NEW
    reddit   *RedditClient    // <-- NEW
    rss      *RSSClient       // <-- NEW
    notifier *NotifyClient    // <-- NEW
    cache    *Cache           // <-- NEW
    vars     map[string]string // <-- NEW (for conditionals)
    // ... existing fields
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
        news:     NewNewsClient(os.Getenv("GNEWS_API_KEY"), serpKey, verbose),
        stocks:   NewStockClient(os.Getenv("FINNHUB_API_KEY"), serpKey, verbose),
        crypto:   NewCryptoClient(verbose),      // no key needed!
        reddit:   NewRedditClient(verbose),       // no key needed!
        rss:      NewRSSClient(verbose),          // no key needed!
        notifier: NewNotifyClient(verbose),
        cache:    NewCache(verbose),
        vars:     make(map[string]string),
        searchKey: serpKey,
        verbose:  verbose,
        procs:    make(map[string]*ProcDef),
    }
}
```

### 2c. Add cases in executeCommand switch

```go
    case "crypto":
        result, err = r.cryptoPrice(ctx, cmd.Arg, input)
    case "reddit":
        result, err = r.redditFetch(ctx, cmd.Arg, cmd.Args, input)
    case "rss":
        result, err = r.rssFetch(ctx, cmd.Arg, input)
    case "notify":
        result, err = r.notify(ctx, cmd.Arg, input)
    case "if":
        result, err = r.executeIf(ctx, cmd, input)
```

### 2d. Handler methods

```go
// ==================== CRYPTO ====================

func (r *Runtime) cryptoPrice(ctx context.Context, arg string, input string) (string, error) {
    query := arg
    if query == "" && input != "" {
        query = strings.TrimSpace(input)
    }
    if query == "" {
        query = "BTC,ETH,SOL" // default
    }

    // Check cache
    return CachedGet(r.cache, "crypto", query, CacheTTLCrypto, func() (string, error) {
        symbols := ParseCryptoSymbols(query)

        // Handle "top N" requests
        if symbols == nil {
            // Parse N from "top 10", "top 20" etc
            n := 10
            fmt.Sscanf(strings.ToLower(query), "top %d", &n)
            prices, err := r.crypto.GetTopN(ctx, n)
            if err != nil {
                return "", err
            }
            return FormatCryptoPrices(prices), nil
        }

        prices, err := r.crypto.GetPrices(ctx, symbols)
        if err != nil {
            return "", err
        }
        return FormatCryptoPrices(prices), nil
    })
}

// ==================== REDDIT ====================

func (r *Runtime) redditFetch(ctx context.Context, arg string, args []string, input string) (string, error) {
    query := arg
    if query == "" && input != "" {
        query = strings.TrimSpace(input)
    }
    if query == "" {
        return "", fmt.Errorf("reddit requires a subreddit or search query")
    }

    var allArgs []string
    if arg != "" {
        allArgs = append(allArgs, arg)
    }
    allArgs = append(allArgs, args...)

    isSub, q, sort := ParseRedditArgs(allArgs...)

    return CachedGet(r.cache, "reddit", q+sort, CacheTTLReddit, func() (string, error) {
        var posts []RedditPost
        var err error

        if isSub {
            posts, err = r.reddit.SearchSubreddit(ctx, q, sort, 10)
        } else {
            posts, err = r.reddit.SearchReddit(ctx, q, sort, 10)
        }

        if err != nil {
            return "", err
        }
        return FormatRedditPosts(posts, q), nil
    })
}

// ==================== RSS ====================

func (r *Runtime) rssFetch(ctx context.Context, feedURL string, input string) (string, error) {
    url := feedURL
    if url == "" && input != "" {
        url = strings.TrimSpace(input)
    }
    if url == "" {
        return ListFeedShortcuts(), nil // show available feeds
    }

    return CachedGet(r.cache, "rss", url, CacheTTLRSS, func() (string, error) {
        items, title, err := r.rss.FetchFeed(ctx, url, 10)
        if err != nil {
            return "", err
        }
        return FormatRSSItems(items, title), nil
    })
}

// ==================== NOTIFY ====================

func (r *Runtime) notify(ctx context.Context, target string, input string) (string, error) {
    if input == "" {
        return "", fmt.Errorf("notify needs piped content. Usage: search \"topic\" -> notify \"slack\"")
    }
    return r.notifier.Send(ctx, input, target)
}

// ==================== IF/ELSE ====================

func (r *Runtime) executeIf(ctx context.Context, cmd *Command, input string) (string, error) {
    // cmd.Arg contains the condition string
    result, err := EvaluateConditionString(cmd.Arg, input, r.vars)
    if err != nil {
        return "", fmt.Errorf("if condition error: %w", err)
    }

    r.log("IF %q -> %v", cmd.Arg, result)

    if result {
        // Execute then branch (the pipe chain)
        if cmd.Pipe != nil {
            return r.executeCommand(ctx, cmd.Pipe, input)
        }
        return input, nil
    }

    // TODO: else branch handling depends on grammar structure
    // For now, pass through on false
    return input, nil
}
```

---

## 3. DSL Usage Examples

### Crypto

```bash
# Specific coins
crypto "BTC,ETH,SOL"
crypto "BTC"

# Top coins by market cap
crypto "top 10"
crypto "top 20"

# Crypto report
crypto "BTC,ETH,SOL,AVAX,LINK" -> ask "which has best 24h performance?" -> notify "slack"
```

### Reddit

```bash
# Browse a subreddit
reddit "r/golang"
reddit "r/golang" "new"       # sort by new
reddit "r/golang" "top"       # sort by top

# Search all of Reddit
reddit "kubernetes best practices"

# Monitor + alert
reddit "r/golang" -> ask "any posts about job openings?" -> notify "slack"
```

### RSS

```bash
# Use shortcuts (no URL needed!)
rss "hn"                 # Hacker News
rss "golang"             # Go blog
rss "techcrunch"
rss "lobsters"
rss "kubernetes"
rss "anthropic"

# Custom URL
rss "https://myblog.com/feed.xml"

# List all shortcuts
rss

# Feed digest
parallel {
  rss "hn"
  rss "golang"
  rss "lobsters"
}
-> merge
-> summarize
-> email "you@gmail.com"
```

### Notify

```bash
# Send to specific channel
weather "NYC" -> notify "slack"
stock "NVDA" -> notify "discord"
crypto "BTC" -> notify "telegram"

# Send to ALL configured channels
news "breaking tech" -> notify

# Alert pipeline
stock "NVDA" -> if "change > 5" { notify "slack" }
```

### Cache

Cache is automatic! Every API call checks cache first. Default TTLs:
- Stocks/Crypto: 1 minute
- News/Reddit/RSS: 5-10 minutes
- Jobs: 1 hour
- Search: 30 minutes

### If/Else

```bash
# Weather-based decisions
weather "NYC" -> if "rain > 50" { ask "umbrella reminder" -> email "you@gmail.com" }

# Stock alerts
stock "NVDA" -> if "change > 5" { notify "slack" }
stock "NVDA" -> if "price > 1000" { ask "should I sell?" -> email "you@gmail.com" }

# Content filtering
reddit "r/golang" -> if contains "hiring" { summarize -> email "you@gmail.com" }
news "AI" -> if contains "breakthrough" { notify "telegram" }

# Crypto alerts
crypto "BTC" -> if "price > 100000" { notify "slack" }
```

---

## 4. translator.go — Add to Gemini's knowledge

```
crypto "SYMBOLS" or crypto "top N" - Get cryptocurrency prices. CoinGecko, free. Examples:
  crypto "BTC,ETH,SOL"
  crypto "top 10"

reddit "r/subreddit" ["sort"] - Browse subreddit (hot/new/top/rising). Or reddit "query" to search. Examples:
  reddit "r/golang"
  reddit "r/golang" "new"
  reddit "kubernetes best practices"

rss "shortcut_or_url" - Read RSS/Atom feeds. Shortcuts: hn, golang, techcrunch, lobsters, kubernetes, anthropic, etc. Examples:
  rss "hn"
  rss "https://myblog.com/feed"

notify "target" - Send piped content to Slack/Discord/Telegram. Targets: slack, discord, telegram, or empty for all. Examples:
  search "topic" -> notify "slack"
  stock "NVDA" -> notify

if "condition" { commands } - Conditional execution. Conditions: >, <, >=, <=, ==, !=, contains, not_contains. Examples:
  weather "NYC" -> if "rain > 50" { notify "slack" }
  stock "NVDA" -> if "change > 5" { email "you@gmail.com" }
```

---

## 5. The Ultimate Daily Briefing

```
# ultimate-briefing.as

parallel {
  weather "San Francisco"
  crypto "BTC,ETH,SOL"
  stock "AAPL,GOOGL,MSFT,NVDA,META"
  news_headlines "technology"
  rss "hn"
  reddit "r/golang" "top"
  job_search "golang contract" "remote"
}
-> merge
-> ask "
  Professional morning briefing with 7 sections:
  1. Weather & outfit suggestion
  2. Crypto snapshot (table)
  3. Market snapshot (table)
  4. Tech headlines (top 5)
  5. Hacker News highlights (top 5)
  6. r/golang trending (top 5)
  7. New job listings (top 5)
  Keep concise. No fluff.
"
-> save "briefing.md"
-> doc_create "Morning Briefing"
-> notify "slack"
-> email "you@gmail.com"
```

---

## 6. Command Count: 42

| Category | Commands | Count |
|----------|----------|-------|
| Core | search, summarize, ask, analyze, save, read, list, merge | 8 |
| Google Workspace | email, calendar, meet, drive_save, doc_create, sheet_create, sheet_append, task, contact_find, youtube_search | 10 |
| Multimodal | image_generate, image_analyze, video_generate, video_analyze, images_to_video | 5 |
| **Data** | job_search, weather, news, news_headlines, stock, **crypto, reddit, rss** | **8** |
| **Notifications** | **notify** | **1** |
| **Control** | parallel, **if** | **2** |
| Other | translate, tts, places_search, youtube_upload, form_create, filter, sort, stdin, etc. | 8+ |

### API Keys Summary

| Command | API | Key Required? | Free Tier |
|---------|-----|---------------|-----------|
| crypto | CoinGecko | **No** | Unlimited (rate limited) |
| reddit | Reddit JSON | **No** | Unlimited (rate limited) |
| rss | Direct HTTP | **No** | Unlimited |
| notify | Webhooks | Webhook URLs | Free |
| weather | Open-Meteo | **No** | Unlimited |
| job_search | SerpAPI | Yes | 100/month |
| news | GNews/SerpAPI | Yes | 100/day |
| stock | Finnhub/SerpAPI | Yes | 60/min |
