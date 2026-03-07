# AgentScript: Adding JOB_SEARCH Command

## Overview
Adds a `job_search` command powered by SerpAPI's Google Jobs engine.
Google Jobs aggregates from Dice, LinkedIn, ZipRecruiter, Indeed, Glassdoor, etc.

## New File
- `job_search.go` — Drop this into your project root alongside `runtime.go`

## Environment Variable
```bash
export SERPAPI_KEY="your-serpapi-key"
# Free tier: 100 searches/month
# Sign up: https://serpapi.com (use Google Jobs engine)
```

---

## 1. grammar.go — Add JOB_SEARCH to the Action regex

Find the Action field in your Command struct and add `job_search`:

```go
// BEFORE:
Action string `@("search"|"summarize"|"ask"|"analyze"|"save"|"read"|"list"|"merge"|"email"|"calendar"|"meet"|"drive_save"|"doc_create"|"sheet_create"|"sheet_append"|"task"|"contact_find"|"youtube_search"|"image_generate"|"image_analyze"|"video_generate"|"video_analyze"|"images_to_video"|"translate"|"tts"|"places_search"|"youtube_upload"|"form_create"|"parallel"|"filter"|"sort"|"stdin"|"hub_pages"|"github_pages_html"|"audio_video_merge"|"video_script")`

// AFTER (add job_search):
Action string `@("job_search"|"search"|"summarize"|"ask"|"analyze"|"save"|"read"|"list"|"merge"|"email"|"calendar"|"meet"|"drive_save"|"doc_create"|"sheet_create"|"sheet_append"|"task"|"contact_find"|"youtube_search"|"image_generate"|"image_analyze"|"video_generate"|"video_analyze"|"images_to_video"|"translate"|"tts"|"places_search"|"youtube_upload"|"form_create"|"parallel"|"filter"|"sort"|"stdin"|"hub_pages"|"github_pages_html"|"audio_video_merge"|"video_script")`
```

---

## 2. runtime.go — Add the job searcher and case handler

### 2a. Add field to Runtime struct

```go
type Runtime struct {
    gemini    *GeminiClient
    google    *GoogleClient
    searcher  *JobSearcher   // <-- ADD THIS
    searchKey string
    verbose   bool
    procs     map[string]*ProcDef
}
```

### 2b. Initialize in NewRuntime (or wherever you construct Runtime)

```go
func NewRuntime(gemini *GeminiClient, google *GoogleClient, verbose bool) *Runtime {
    serpKey := os.Getenv("SERPAPI_KEY")
    return &Runtime{
        gemini:    gemini,
        google:    google,
        searcher:  NewJobSearcher(serpKey, verbose),  // <-- ADD THIS
        searchKey: serpKey,
        verbose:   verbose,
        procs:     make(map[string]*ProcDef),
    }
}
```

### 2c. Add case in executeCommand switch

Find the big `switch strings.ToLower(cmd.Action)` block and add:

```go
    case "job_search":
        result, err = r.jobSearch(ctx, cmd.Arg, cmd.Args, input)
```

### 2d. Add the handler method

```go
// jobSearch performs a job search using SerpAPI Google Jobs
func (r *Runtime) jobSearch(ctx context.Context, arg string, args []string, input string) (string, error) {
    if r.searcher == nil || r.searcher.serpAPIKey == "" {
        return "", fmt.Errorf("SERPAPI_KEY not set - required for job_search. Get one at https://serpapi.com")
    }

    // Build config from arguments
    var allArgs []string
    if arg != "" {
        allArgs = append(allArgs, arg)
    }
    allArgs = append(allArgs, args...)

    // If input is piped in, use it as additional context
    if len(allArgs) == 0 && input != "" {
        allArgs = []string{input}
    }

    config := ParseJobSearchArgs(allArgs...)
    r.log("JOB_SEARCH: query=%q location=%q type=%q", config.Query, config.Location, config.EmploymentType)

    jobs, err := r.searcher.Search(ctx, config)
    if err != nil {
        return "", fmt.Errorf("job search failed: %w", err)
    }

    // Return formatted results (table format for piping)
    return FormatJobResults(jobs), nil
}
```

---

## 3. DSL Usage

### Basic search
```bash
./agentscript -e 'job_search "golang software engineer contract"'
```

### With location
```bash
./agentscript -e 'job_search "golang contract" "remote"'
```

### With employment type
```bash
./agentscript -e 'job_search "go developer" "New York" "CONTRACTOR"'
```

### Pipeline: search → filter with Gemini → email
```bash
./agentscript -e 'job_search "golang software engineer contract" -> ask "filter to remote only, sort by salary, format as clean table" -> email "you@gmail.com"'
```

### Parallel multi-query search
```bash
./agentscript -e '
parallel {
  job_search "golang contract developer" "remote"
  job_search "go software engineer contract" "remote"  
  job_search "golang microservices contract" "remote"
}
-> merge
-> ask "deduplicate by company+title. Format as table with: Title, Company, Location, Salary, Source, Apply Link. Sort by salary descending. Flag any paying over $80/hr"
-> doc_create "Go Contract Jobs Report"
-> email "you@gmail.com"
'
```

### Daily job hunt script (save as jobs.as)
```
# jobs.as - Daily Go contract job search

parallel {
  job_search "golang software engineer contract" "remote"
  job_search "go developer contract W2" "remote"
  job_search "golang backend engineer contract" "remote"
  job_search "go microservices kubernetes contract" "remote"
}
-> merge
-> ask "
  1. Deduplicate by company + title
  2. Remove any full-time positions (contract/W2/C2C only)
  3. Sort by salary/rate descending
  4. Format as markdown table: Title | Company | Location | Rate | Source | Apply Link
  5. Flag positions mentioning: Kubernetes, AWS, microservices, gRPC
  6. Add a summary section at top with total count and salary range
"
-> save "go-jobs-today.md"
-> doc_create "Go Contract Jobs"
-> drive_save "Jobs/go-contracts.md"
-> email "you@gmail.com"
```

Run it:
```bash
./agentscript -f jobs.as
```

### Natural language mode
```bash
./agentscript -n "find all remote golang contract jobs paying over 70 per hour and email them to me"
```

---

## 4. translator.go — Teach Gemini about job_search

Add to your translator prompt (where you list available commands):

```
job_search "query" ["location"] ["CONTRACTOR|FULLTIME|PARTTIME|INTERN"] - Search for jobs using Google Jobs (aggregates Dice, LinkedIn, ZipRecruiter, Indeed, Glassdoor). Examples:
  job_search "golang contract"
  job_search "python developer" "remote"
  job_search "react engineer" "San Francisco" "CONTRACTOR"
```

---

## 5. Total command count: 35

Updated command list:
- Core (8): search, summarize, ask, analyze, save, read, list, merge
- Google Workspace (10): email, calendar, meet, drive_save, doc_create, sheet_create, sheet_append, task, contact_find, youtube_search
- Multimodal (5): image_generate, image_analyze, video_generate, video_analyze, images_to_video
- **Jobs (1): job_search** ← NEW
- Control (1): parallel
- Other (10): translate, tts, places_search, youtube_upload, form_create, filter, sort, stdin, hub_pages, github_pages_html, audio_video_merge, video_script
