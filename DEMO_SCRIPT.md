# AgentScript Demo Video Script
# Target: 3-5 minutes
# Tone: Fast-paced, developer-focused, wow-factor moments

## INTRO (15 seconds)
---
[Screen: Terminal, dark theme]

NARRATION:
"What if chaining AI, APIs, and automation was as simple as Unix pipes?"

[Type on screen:]
search "AI trends" -> summarize -> email "you@gmail.com"

"This is AgentScript. 45 commands. One syntax. Zero boilerplate."

---

## SECTION 1: The Basics (45 seconds)
---
[Terminal]

NARRATION:
"Let's start simple."

DEMO:
```bash
./agentscript -e 'ask "explain kubernetes in 3 sentences"'
```
[Show output]

"Now chain it:"

```bash
./agentscript -e 'search "kubernetes security best practices" -> summarize -> save "k8s-security.md"'
```
[Show output, then cat the saved file]

"Search, summarize, save. Three commands, one line."

---

## SECTION 2: Parallel Execution (45 seconds)
---
NARRATION:
"But real workflows aren't linear. AgentScript runs branches in parallel."

DEMO:
```bash
./agentscript -v -e '
parallel {
  search "AWS vs Azure" -> analyze
  search "GCP vs Azure" -> analyze
}
-> merge
-> ask "which cloud is best for startups?"
'
```

[Show verbose output with parallel goroutines]

"Two searches, running concurrently, merged, then analyzed. 
That's the power of parallel { }."

---

## SECTION 3: Real-World Data — No API Keys (60 seconds)
---
NARRATION:
"Four commands work with zero API keys. Let me show you."

DEMO 1 — Weather:
```bash
./agentscript -e 'weather "Tokyo"'
```
[Show current conditions + 7-day forecast]

DEMO 2 — Crypto:
```bash
./agentscript -e 'crypto "BTC,ETH,SOL"'
```
[Show prices table]

DEMO 3 — Reddit:
```bash
./agentscript -e 'reddit "r/golang"'
```
[Show top posts]

DEMO 4 — RSS:
```bash
./agentscript -e 'rss "hn"'
```
[Show Hacker News feed]

"Weather, crypto, Reddit, RSS — all free, no keys, no setup."

---

## SECTION 4: The Killer Pipeline (60 seconds)
---
NARRATION:
"Here's where it gets real. One script that runs my entire morning briefing."

[Show daily-briefing.as file]

DEMO:
```bash
./agentscript -f examples/daily-briefing.as
```

[Show parallel execution: weather + crypto + stocks + news + HN + Reddit + jobs all running]

[Show merged output being processed by Gemini]

[Show email being sent]

"Weather, crypto, stocks, tech headlines, Hacker News, Reddit, 
and job listings — researched, merged, formatted, and emailed. 
One script. Under 20 lines."

---

## SECTION 5: Conditional Logic + Alerts (30 seconds)
---
NARRATION:
"AgentScript isn't just pipelines. It has conditionals too."

DEMO:
```bash
./agentscript -e 'stock "NVDA" -> if "change > 5" { notify "slack" }'
```

```bash
./agentscript -e 'weather "NYC" -> if "rain > 50" { email "you@gmail.com" }'
```

"If NVIDIA moves more than 5%, Slack me. If rain's likely, email me."

---

## SECTION 6: Natural Language Mode (30 seconds)
---
NARRATION:
"And the cherry on top — you don't even need to learn the syntax."

DEMO:
```bash
./agentscript -n "find remote golang contract jobs and email me the top 10"
```

[Show Gemini translating English → DSL]
[Show DSL executing]

"Describe what you want in English. Gemini translates it to AgentScript. 
Then AgentScript executes it."

---

## SECTION 7: Multimodal — Images + Video (30 seconds)
---
NARRATION:
"AgentScript also generates images with Imagen 4 and videos with Veo 3.1."

DEMO:
```bash
./agentscript -e '
parallel {
  image_generate "futuristic city at sunset"
  image_generate "robot in a garden"
}
-> images_to_video
-> save "showcase.mp4"
'
```

[Show generated images, then the video]

---

## OUTRO (15 seconds)
---
NARRATION:
"AgentScript. 45 commands. Parallel execution. Conditional logic.
Natural language mode. Built with Go and Participle. 
Powered by Gemini."

[Show on screen:]
github.com/vinodhalaharvi/agentscript

"Because writing 500 lines of Python to chain APIs was always ridiculous."

[END]

---

## RECORDING TIPS
1. Use a clean terminal (dark theme, large font ~18pt)
2. Pre-set all env vars so nothing fails live
3. Have all examples cached so responses are fast
4. Use `script` or OBS to record
5. Speed up waiting parts in post-production
6. Total runtime target: 3-4 minutes (fast cuts between sections)
7. If Gemini rate-limits, edit around it or use cached results
