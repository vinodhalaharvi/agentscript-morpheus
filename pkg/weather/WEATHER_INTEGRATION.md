# AgentScript: Adding WEATHER Command

## Overview
Adds a `weather` command using **Open-Meteo API** — completely free, no API key needed.
Returns current conditions, hourly forecast (24h), and 7-day forecast.

## New File
- `weather.go` — Drop into your project root alongside `runtime.go`

## No API Key Required!
Open-Meteo is free for non-commercial use. Zero setup.

---

## 1. grammar.go — Add weather to the Action regex

```go
// Add "weather" to the action list:
Action string `@("weather"|"job_search"|"search"|"summarize"|...)`
```

---

## 2. runtime.go — Add the weather client and case handler

### 2a. Add field to Runtime struct

```go
type Runtime struct {
    gemini    *GeminiClient
    google    *GoogleClient
    searcher  *JobSearcher
    weather   *WeatherClient   // <-- ADD THIS
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
        gemini:    gemini,
        google:    google,
        searcher:  NewJobSearcher(serpKey, verbose),
        weather:   NewWeatherClient(verbose),  // <-- ADD THIS (no key needed!)
        searchKey: serpKey,
        verbose:   verbose,
        procs:     make(map[string]*ProcDef),
    }
}
```

### 2c. Add case in executeCommand switch

```go
    case "weather":
        result, err = r.getWeather(ctx, cmd.Arg, input)
```

### 2d. Add the handler method

```go
// getWeather fetches weather for a location
func (r *Runtime) getWeather(ctx context.Context, location string, input string) (string, error) {
    if r.weather == nil {
        r.weather = NewWeatherClient(r.verbose)
    }

    // Use argument or piped input as location
    loc := location
    if loc == "" && input != "" {
        loc = strings.TrimSpace(input)
    }
    if loc == "" {
        return "", fmt.Errorf("weather requires a location, e.g.: weather \"New York\"")
    }

    r.log("WEATHER: %q", loc)

    data, err := r.weather.GetWeather(ctx, loc)
    if err != nil {
        return "", fmt.Errorf("weather lookup failed: %w", err)
    }

    return FormatWeather(data), nil
}
```

---

## 3. DSL Usage

### Basic weather check
```bash
./agentscript -e 'weather "New York"'
```

### Weather for a trip
```bash
./agentscript -e 'weather "Tokyo" -> ask "should I pack an umbrella this week?"'
```

### Multi-city comparison
```bash
./agentscript -e '
parallel {
  weather "San Francisco"
  weather "Austin"
  weather "Seattle"
  weather "Denver"
}
-> merge
-> ask "compare these cities. Which has the best weather this week for outdoor activities? Format as a comparison table"
'
```

### Weather + job search combo
```bash
./agentscript -e '
parallel {
  job_search "golang contract" "remote"
  weather "San Francisco"
  weather "Austin"
  weather "New York"
}
-> merge
-> ask "match the job locations with weather forecasts. Which cities have both good jobs AND good weather this week?"
-> email "you@gmail.com"
'
```

### Travel planning
```bash
./agentscript -e '
weather "Paris"
-> ask "I am traveling there next week. What should I pack? Any weather warnings?"
-> email "you@gmail.com"
'
```

### Daily briefing script (save as daily-briefing.as)
```
# daily-briefing.as - Morning briefing with weather + jobs

parallel {
  weather "your-city"
  job_search "golang contract" "remote"
  search "tech news today"
}
-> merge
-> ask "
  Create a morning briefing with 3 sections:
  1. Weather summary and what to wear
  2. Top 5 new job listings
  3. Top 3 tech news highlights
  Format cleanly with headers.
"
-> email "you@gmail.com"
```

### Natural language mode
```bash
./agentscript -n "what's the weather in Tokyo this week"
```

---

## 4. translator.go — Teach Gemini about weather

Add to your translator prompt:

```
weather "location" - Get current weather, hourly forecast (24h), and 7-day forecast for any city. Examples:
  weather "New York"
  weather "Tokyo"
  weather "London, UK"
```

---

## 5. Features

- **Free** — Open-Meteo, no API key needed
- **Geocoding** — Accepts city names, resolves to coordinates automatically
- **Current conditions** — Temperature, feels like, humidity, wind, condition
- **24-hour hourly forecast** — Temperature, rain chance, wind
- **7-day daily forecast** — High/low, rain chance, sunrise/sunset
- **WMO weather codes** — Mapped to human-readable conditions
- **Fahrenheit + MPH** — Default units (easy to change in code)
- **Pipe-friendly** — Output works great with `-> ask`, `-> summarize`, `-> email`

## 6. Total command count: 36
