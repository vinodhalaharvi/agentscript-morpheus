# AgentScript

**A domain-specific language for chaining AI and APIs in simple, readable pipelines.**

One line replaces hundreds of lines of code.

```
search "AI trends" -> summarize -> email "you@gmail.com"
```

## What It Does

AgentScript lets you chain Gemini AI with 45+ integrations using Unix-pipe-style syntax. Research topics, generate images and videos, send emails, check stocks, monitor Reddit, read RSS feeds, get weather forecasts, search jobs — all in one script.

```bash
# Morning briefing in one command
parallel {
  weather "San Francisco"
  crypto "BTC,ETH,SOL"
  stock "AAPL,NVDA,MSFT"
  news_headlines "technology"
  rss "hn"
  reddit "r/golang"
  job_search "golang contract" "remote"
}
-> merge
-> ask "morning briefing with weather, markets, headlines, and jobs"
-> notify "slack"
-> email "you@gmail.com"
```

## Quick Start

```bash
git clone https://github.com/vinodhalaharvi/agentscript
cd agentscript
go mod tidy
go build -o agentscript .

export GEMINI_API_KEY="your-key"

# Try it
./agentscript -e 'ask "hello world"'
./agentscript -e 'search "golang trends" -> summarize'
./agentscript -e 'crypto "BTC,ETH,SOL"'
./agentscript -e 'weather "New York"'
```

## The Grammar

```
Program     = Statement*
Statement   = (If | Parallel | ForEach | Command) ("->" Statement)?
Parallel    = "parallel" "{" Statement* "}"
If          = "if" String "{" Statement* "}" ("else" "{" Statement* "}")?
ForEach     = "foreach" String?
Command     = Action String*
```

That's it. Six lines define the entire language.

## All 45 Commands

### Core
| Command | Example | Description |
|---------|---------|-------------|
| `search` | `search "topic"` | Web search via Gemini |
| `summarize` | `-> summarize` | Summarize piped input |
| `ask` | `-> ask "question"` | Ask with context |
| `analyze` | `-> analyze "focus"` | Deep analysis |
| `save` | `-> save "file.md"` | Save to local file |
| `read` | `read "file.txt"` | Read local file |
| `list` | `list "."` | List directory |
| `merge` | `-> merge` | Merge parallel results |

### Data
| Command | Example | API | Key? |
|---------|---------|-----|------|
| `weather` | `weather "NYC"` | Open-Meteo | **No** |
| `crypto` | `crypto "BTC,ETH"` | CoinGecko | **No** |
| `reddit` | `reddit "r/golang"` | Reddit JSON | **No** |
| `rss` | `rss "hn"` | Direct HTTP | **No** |
| `news` | `news "AI"` | GNews | Yes |
| `news_headlines` | `news_headlines "tech"` | GNews | Yes |
| `stock` | `stock "AAPL,NVDA"` | Finnhub | Yes |
| `job_search` | `job_search "golang"` | SerpAPI | Yes |
| `twitter` | `twitter "golang"` | Twitter API | Yes |

### Google Workspace
| Command | Example | API |
|---------|---------|-----|
| `email` | `-> email "to@gmail.com"` | Gmail |
| `calendar` | `-> calendar "Meeting 2pm"` | Calendar |
| `meet` | `-> meet "Sprint Review"` | Calendar+Meet |
| `drive_save` | `-> drive_save "path/file"` | Drive |
| `doc_create` | `-> doc_create "Title"` | Docs |
| `sheet_create` | `-> sheet_create "Title"` | Sheets |
| `sheet_append` | `-> sheet_append "id/sheet"` | Sheets |
| `task` | `-> task "todo"` | Tasks |
| `contact_find` | `contact_find "John"` | People |
| `youtube_search` | `youtube_search "query"` | YouTube |

### Multimodal (Gemini)
| Command | Example | Model |
|---------|---------|-------|
| `image_generate` | `image_generate "robot"` | Imagen 4 |
| `image_analyze` | `image_analyze "describe"` | Gemini |
| `video_generate` | `video_generate "sunset"` | Veo 3.1 |
| `video_analyze` | `video_analyze "summarize"` | Gemini |
| `images_to_video` | `-> images_to_video` | ffmpeg |
| `tts` | `-> tts "en"` | Gemini TTS |
| `translate` | `-> translate "Japanese"` | Gemini |

### Notifications
| Command | Example | Service |
|---------|---------|---------|
| `email` | `-> email "you@gmail.com"` | Gmail |
| `notify` | `-> notify "slack"` | Slack/Discord/Telegram |
| `whatsapp` | `-> whatsapp "+1234567890"` | Twilio |

### Control Flow
| Command | Example | Description |
|---------|---------|-------------|
| `parallel` | `parallel { ... }` | Run branches concurrently |
| `if` | `if "rain > 50" { ... }` | Conditional execution |
| `foreach` | `-> foreach "line"` | Iterate over items |

## Example Pipelines

### Daily Job Hunt
```
parallel {
  job_search "golang contract" "remote"
  job_search "go microservices" "remote"
}
-> merge
-> ask "deduplicate, format as table, sort by rate"
-> email "you@gmail.com"
```

### Stock Alert
```
stock "NVDA" -> if "change > 5" { notify "slack" }
```

### Tech Digest
```
parallel {
  rss "hn"
  rss "lobsters"
  reddit "r/golang" "top"
  news_headlines "technology"
}
-> merge -> summarize -> email "you@gmail.com"
```

### Multimodal Pipeline
```
search "butterflies migration"
-> summarize
-> tts "en"
-> image_generate "monarch butterflies migrating"
-> images_to_video
-> youtube_upload "Butterfly Migration"
```

### Natural Language Mode
```bash
./agentscript -n "find golang jobs and email me a summary"
# Gemini translates English -> DSL -> executes
```

## RSS Feed Shortcuts

No URL needed — just use the shortcut name:

| Shortcut | Feed |
|----------|------|
| `hn` | Hacker News |
| `golang` | Go Blog |
| `lobsters` | Lobste.rs |
| `techcrunch` | TechCrunch |
| `kubernetes` | Kubernetes Blog |
| `anthropic` | Anthropic Blog |
| `github-blog` | GitHub Blog |
| `reddit-golang` | r/golang RSS |
| `dev-to` | Dev.to |

30+ shortcuts available. Run `rss` with no args to see all.

## Architecture

```
Natural Language ─── Gemini translates ──→ AgentScript DSL
                                              │
                                         Participle parser
                                              │
                                             AST
                                              │
                                        Runtime executor
                                        ┌─────┴─────┐
                                   Sequential    Parallel
                                        │            │
                              ┌────┬────┴────┐   goroutines
                              │    │    │    │
                           Gemini APIs  Files Cache
```

## Project Structure

```
agentscript/
├── main.go           # CLI entry point (expression, file, REPL, natural language modes)
├── grammar.go        # Participle grammar definition
├── runtime.go        # Command execution engine
├── client.go         # Gemini API client (text, Imagen 4, Veo 3.1, TTS)
├── google.go         # Google Workspace integrations
├── translator.go     # Natural language → DSL via Gemini
├── job_search.go     # SerpAPI Google Jobs
├── weather.go        # Open-Meteo (free, no key)
├── news.go           # GNews + SerpAPI fallback
├── stock.go          # Finnhub + SerpAPI fallback
├── crypto.go         # CoinGecko (free, no key)
├── reddit.go         # Reddit public JSON (free, no key)
├── rss.go            # RSS/Atom feed reader (free, no key)
├── twitter.go        # Twitter/X API v2
├── whatsapp.go       # Twilio WhatsApp
├── notify.go         # Slack/Discord/Telegram webhooks
├── cache.go          # File-based API result caching
├── retry.go          # Exponential backoff retry
├── conditional.go    # If/else logic
├── loop.go           # ForEach iteration
├── agentscript_test.go # Test suite
├── Makefile
└── examples/
    ├── daily-briefing.as
    ├── go-job-hunt.as
    ├── showcase.as
    └── ...
```

## Environment Variables

```bash
# Required
export GEMINI_API_KEY="..."

# Data APIs (all have free tiers)
export SERPAPI_KEY="..."           # Jobs, news/stock fallback (100/mo free)
export FINNHUB_API_KEY="..."      # Stocks (60/min free)
export GNEWS_API_KEY="..."        # News (100/day free)
export TWITTER_BEARER_TOKEN="..." # Twitter search

# Google Workspace (OAuth)
export GOOGLE_CREDENTIALS_FILE="credentials.json"

# Notifications
export SLACK_WEBHOOK_URL="..."
export DISCORD_WEBHOOK_URL="..."
export TELEGRAM_BOT_TOKEN="..."
export TELEGRAM_CHAT_ID="..."

# WhatsApp (Twilio)
export TWILIO_ACCOUNT_SID="..."
export TWILIO_AUTH_TOKEN="..."
export TWILIO_WHATSAPP_FROM="whatsapp:+14155238886"

# 4 commands need ZERO keys: weather, crypto, reddit, rss
```

## Running

```bash
# Expression mode
./agentscript -e 'search "topic" -> summarize'

# File mode
./agentscript -f examples/daily-briefing.as

# REPL mode
./agentscript -i

# Natural language mode
./agentscript -n "research AI and email me a summary"

# Verbose mode (debug)
./agentscript -v -e 'crypto "BTC"'
```

## Testing

```bash
# Unit tests (no keys needed)
go test -run "TestParse|TestFormat|TestCache|TestRetry|TestLoop" -v

# Integration tests (keys optional, skipped if missing)
go test -v

# Specific integration
SERPAPI_KEY="..." go test -run TestJobSearch -v
```

## Built With

- [Go 1.22](https://golang.org/) — Runtime engine
- [Participle v2](https://github.com/alecthomas/participle) — Parser generator
- [Gemini API](https://ai.google.dev/) — Text, Imagen 4, Veo 3.1, TTS
- [Google Workspace APIs](https://developers.google.com/workspace) — Gmail, Calendar, Drive, Docs, Sheets, Forms, YouTube, Tasks
- [Open-Meteo](https://open-meteo.com/) — Weather (free)
- [CoinGecko](https://www.coingecko.com/) — Crypto prices (free)
- [SerpAPI](https://serpapi.com/) — Job search, news, finance
- [Finnhub](https://finnhub.io/) — Stock quotes
- [GNews](https://gnews.io/) — News articles
- [Twilio](https://www.twilio.com/) — WhatsApp messaging

## Links

- **Demo:** https://youtube.com/watch?v=6yJa5LiUS9U
- **DevPost:** https://devpost.com/software/agent-script
- **Author:** [Vinod Halaharvi](https://github.com/vinodhalaharvi)

## License

MIT
