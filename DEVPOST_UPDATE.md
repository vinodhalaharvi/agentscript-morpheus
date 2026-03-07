# DevPost Submission: AgentScript

---

## Project Name
AgentScript

## Tagline
A DSL where `search "topic" -> summarize -> email "you@gmail.com"` just works.

---

## Inspiration

Every project needed the same pattern: search something, summarize with AI, generate an image, send an email. Why couldn't this be as simple as Unix pipes? That's when AgentScript was born — a DSL where complex AI workflows become one-liners.

---

## What it does

AgentScript is a domain-specific language that chains Gemini AI with 45+ integrations in simple, readable pipelines. With one script you can:

- Research topics and generate reports
- Create images (Imagen 4) and videos (Veo 3.1)
- Convert text to speech with natural voices
- Check weather, crypto prices, stock quotes — no API keys needed
- Monitor Reddit, Hacker News, and RSS feeds
- Search for jobs across Dice, LinkedIn, Indeed, and more
- Send emails, Slack messages, Discord alerts, WhatsApp messages
- Create Google Docs, Sheets, Forms, Calendar events
- Upload to YouTube including Shorts
- Run tasks in parallel for maximum speed
- Use conditional logic for smart alerts
- Describe workflows in plain English — Gemini translates to DSL

One line replaces hundreds of lines of code.

---

## How we built it

- **Go 1.22** for the runtime engine
- **Participle v2** for parsing the DSL grammar into an AST
- **Gemini 2.5 Flash** for text generation, summarization, translation, and natural language → DSL translation
- **Imagen 4** for image generation
- **Veo 3.1** for video generation
- **Gemini TTS** for text-to-speech
- **Google Workspace APIs** — Gmail, Calendar, Drive, Docs, Sheets, Forms, YouTube, Tasks, People
- **Open-Meteo** — Weather forecasts (free, no key)
- **CoinGecko** — Crypto prices (free, no key)
- **Reddit JSON API** — Subreddit browsing and search (free, no key)
- **RSS/Atom** — 30+ built-in feed shortcuts (free, no key)
- **SerpAPI** — Job search via Google Jobs engine
- **Finnhub** — Real-time stock quotes
- **GNews** — News article search
- **Twitter/X API v2** — Tweet search
- **Twilio** — WhatsApp messaging
- **Slack/Discord/Telegram** — Webhook notifications
- **ffmpeg** for audio/video processing
- **OAuth 2.0** for secure Google API authentication
- **File-based caching** with configurable TTLs to minimize API calls
- **Exponential backoff retry** for rate-limited APIs

The architecture cleanly separates parsing (grammar.go), execution (runtime.go), API clients, and caching — each command is a standalone Go file that drops into the project.

---

## Challenges we ran into

- **Veo API quotas** — Limited to 10 videos/day, implemented ffmpeg-based image_audio_merge as fallback
- **Google Forms API** — Index 0 gets ignored in protobuf, required ForceSendFields workaround
- **Gemini TTS format** — Returns raw PCM, not WAV. Required ffmpeg for conversion
- **OAuth scope management** — Adding new Google APIs required re-authentication with expanded scopes
- **Rate limiting** — Free tier Gemini (429 errors) led to building a full retry system with exponential backoff
- **Parallel execution** — Coordinating concurrent API calls while maintaining pipeline flow and error handling
- **RSS parsing** — Had to support both RSS 2.0 and Atom formats with different XML structures
- **Conditional evaluation** — Extracting numeric values from unstructured pipeline output for if/else comparisons

---

## Accomplishments that we are proud of

- **45 working commands** spanning research, data, multimedia, notifications, and Google Workspace
- **4 commands need zero API keys** — weather, crypto, Reddit, RSS work out of the box
- **Natural language mode** — Describe what you want in English, AgentScript translates to DSL
- **Parallel execution** — `parallel { }` runs multiple branches concurrently with goroutines
- **Conditional logic** — `if "rain > 50" { notify "slack" }` for smart alerting
- **ForEach iteration** — Process lists item by item through any pipeline
- **Auto-retry with backoff** — Handles rate limits gracefully across all APIs
- **File-based caching** — Configurable TTLs per data type (1min stocks → 1hr jobs)
- **30+ RSS shortcuts** — `rss "hn"` just works, no URLs needed
- **End-to-end video pipeline** — Search → Summarize → TTS → Image → Video → YouTube upload
- **Morning briefing script** — Weather + crypto + stocks + news + HN + Reddit + jobs → email, one command
- **Full test suite** — Unit tests for parsing/formatting + integration tests for all API commands

---

## What we learned

- Gemini's multimodal capabilities are incredibly powerful when chained together
- DSLs dramatically lower the barrier to AI automation
- Google's API ecosystem is vast but OAuth complexity is real
- Free APIs (Open-Meteo, CoinGecko, Reddit JSON) are surprisingly capable
- Caching and retry logic are essential for any multi-API system
- Sometimes ffmpeg is the answer (audio conversion, video merging)
- A 6-line grammar can express remarkably complex workflows
- Good error messages matter more than perfect code

---

## What's next for AgentScript

- **MCP server** — Expose AgentScript as Model Context Protocol tools so Claude and other AI assistants can run pipelines
- **Web UI** — Browser dashboard to write, run, and schedule scripts
- **Plugin system** — Custom commands via Go plugins or WASM
- **Schedule command** — Built-in cron for recurring pipelines (`schedule "daily 9am" { ... }`)
- **Variables** — `$portfolio = "AAPL,NVDA,MSFT"` for reusable configs
- **GitHub integration** — Trending repos, issue creation, PR monitoring
- **LinkedIn posting** — Share pipeline outputs directly

---

## Built With

go, participle, gemini-api, imagen-4, veo-3, google-workspace, open-meteo, coingecko, serpapi, finnhub, gnews, twitter-api, twilio, slack, discord, telegram, ffmpeg, oauth2

---

## Try It Out

- **Demo:** https://youtube.com/watch?v=6yJa5LiUS9U
- **Code:** https://github.com/vinodhalaharvi/agentscript
- **DevPost:** https://devpost.com/software/agent-script

---

## Describe your contribution

Solo project. Designed the DSL syntax and grammar, built the Participle parser, implemented all 45 commands, integrated Gemini APIs (text, Imagen 4, Veo 3.1, TTS), connected Google Workspace APIs (Gmail, Calendar, Drive, Docs, Sheets, Forms, YouTube, Tasks, People), added external data APIs (weather, crypto, Reddit, RSS, news, stocks, jobs, Twitter), built notification system (Slack, Discord, Telegram, WhatsApp), implemented caching, retry logic, conditional execution, parallel processing, foreach loops, natural language translation, and wrote the complete test suite and demo examples.
