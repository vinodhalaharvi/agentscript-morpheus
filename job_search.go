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

// JobResult represents a single job listing
type JobResult struct {
	Title       string   `json:"title"`
	Company     string   `json:"company_name"`
	Location    string   `json:"location"`
	Via         string   `json:"via"`
	Description string   `json:"description"`
	PostedAt    string   `json:"posted_at"`
	Schedule    string   `json:"schedule_type"`
	Salary      string   `json:"salary"`
	Link        string   `json:"link"`
	ApplyLinks  []string `json:"apply_links"`
	Extensions  []string `json:"extensions"`
	JobID       string   `json:"job_id"`
}

// JobSearchConfig holds search parameters
type JobSearchConfig struct {
	Query          string
	Location       string
	EmploymentType string // FULLTIME, PARTTIME, CONTRACTOR, INTERN
	PostedAge      string // today, 3days, week, month
	Chips          string // additional filter chips from SerpAPI
	NumPages       int    // how many pages to fetch (10 results per page)
}

// ParseJobSearchArgs parses the DSL argument into a JobSearchConfig
// Supports formats:
//
//	job_search "golang contract"
//	job_search "golang contract" "remote"
//	job_search "golang contract" "New York" "CONTRACTOR"
func ParseJobSearchArgs(args ...string) JobSearchConfig {
	config := JobSearchConfig{
		NumPages: 2, // default: fetch 20 results
	}

	if len(args) >= 1 {
		config.Query = args[0]
	}
	if len(args) >= 2 {
		config.Location = args[1]
	}
	if len(args) >= 3 {
		// Check if it's an employment type
		upper := strings.ToUpper(args[2])
		switch upper {
		case "FULLTIME", "PARTTIME", "CONTRACTOR", "INTERN":
			config.EmploymentType = upper
		default:
			// Treat as additional query context
			config.Query += " " + args[2]
		}
	}

	// Auto-detect "contract" in query and set employment type
	queryLower := strings.ToLower(config.Query)
	if config.EmploymentType == "" {
		if strings.Contains(queryLower, "contract") {
			config.EmploymentType = "CONTRACTOR"
		}
	}

	return config
}

// JobSearcher handles job search API calls
type JobSearcher struct {
	serpAPIKey string
	client     *http.Client
	verbose    bool
}

// NewJobSearcher creates a new job searcher
func NewJobSearcher(serpAPIKey string, verbose bool) *JobSearcher {
	return &JobSearcher{
		serpAPIKey: serpAPIKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		verbose: verbose,
	}
}

func (js *JobSearcher) log(format string, args ...any) {
	if js.verbose {
		fmt.Printf("[JOB_SEARCH] "+format+"\n", args...)
	}
}

// Search performs a job search using SerpAPI Google Jobs engine
func (js *JobSearcher) Search(ctx context.Context, config JobSearchConfig) ([]JobResult, error) {
	if js.serpAPIKey == "" {
		return nil, fmt.Errorf("SERPAPI_KEY environment variable required for job_search")
	}

	var allJobs []JobResult

	for page := 0; page < config.NumPages; page++ {
		jobs, err := js.searchPage(ctx, config, page)
		if err != nil {
			if page > 0 {
				// Got some results from earlier pages, don't fail
				js.log("Page %d failed: %v (returning %d results so far)", page, err, len(allJobs))
				break
			}
			return nil, err
		}
		allJobs = append(allJobs, jobs...)

		if len(jobs) < 10 {
			break // No more results
		}

		// Rate limit between pages
		if page < config.NumPages-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	js.log("Total jobs found: %d", len(allJobs))
	return allJobs, nil
}

func (js *JobSearcher) searchPage(ctx context.Context, config JobSearchConfig, page int) ([]JobResult, error) {
	params := url.Values{}
	params.Set("engine", "google_jobs")
	params.Set("api_key", js.serpAPIKey)
	params.Set("q", config.Query)

	if config.Location != "" {
		params.Set("location", config.Location)
	}

	if config.EmploymentType != "" {
		// SerpAPI uses chips parameter for employment type filter
		params.Set("chips", fmt.Sprintf("employment_type:%s", config.EmploymentType))
	}

	if config.PostedAge != "" {
		datePosted := config.PostedAge
		switch config.PostedAge {
		case "today":
			datePosted = "today"
		case "3days":
			datePosted = "3days"
		case "week":
			datePosted = "week"
		case "month":
			datePosted = "month"
		}
		if config.EmploymentType != "" {
			params.Set("chips", fmt.Sprintf("employment_type:%s,date_posted:%s", config.EmploymentType, datePosted))
		} else {
			params.Set("chips", fmt.Sprintf("date_posted:%s", datePosted))
		}
	}

	if page > 0 {
		params.Set("start", fmt.Sprintf("%d", page*10))
	}

	searchURL := "https://serpapi.com/search.json?" + params.Encode()
	js.log("Fetching page %d: %s", page, config.Query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := js.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("SerpAPI error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for error
	if errMsg, ok := result["error"].(string); ok {
		return nil, fmt.Errorf("SerpAPI error: %s", errMsg)
	}

	// Extract jobs
	jobsRaw, ok := result["jobs_results"].([]any)
	if !ok || len(jobsRaw) == 0 {
		js.log("No jobs found on page %d", page)
		return nil, nil
	}

	var jobs []JobResult
	for _, raw := range jobsRaw {
		jobMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		job := JobResult{
			Title:    getString(jobMap, "title"),
			Company:  getString(jobMap, "company_name"),
			Location: getString(jobMap, "location"),
			Via:      getString(jobMap, "via"),
			JobID:    getString(jobMap, "job_id"),
		}

		// Extract description (truncate for readability)
		desc := getString(jobMap, "description")
		if len(desc) > 500 {
			desc = desc[:500] + "..."
		}
		job.Description = desc

		// Extract extensions (posted time, schedule, etc.)
		if exts, ok := jobMap["extensions"].([]any); ok {
			for _, ext := range exts {
				if s, ok := ext.(string); ok {
					job.Extensions = append(job.Extensions, s)
				}
			}
		}

		// Extract detected extensions
		if detected, ok := jobMap["detected_extensions"].(map[string]any); ok {
			if posted, ok := detected["posted_at"].(string); ok {
				job.PostedAt = posted
			}
			if schedule, ok := detected["schedule_type"].(string); ok {
				job.Schedule = schedule
			}
			if salary, ok := detected["salary"].(string); ok {
				job.Salary = salary
			}
		}

		// Extract apply links
		if applyOpts, ok := jobMap["apply_options"].([]any); ok {
			for _, opt := range applyOpts {
				if optMap, ok := opt.(map[string]any); ok {
					link := getString(optMap, "link")
					title := getString(optMap, "title")
					if link != "" {
						job.ApplyLinks = append(job.ApplyLinks, fmt.Sprintf("%s: %s", title, link))
					}
				}
			}
		}

		// Fallback: get the main link
		if link := getString(jobMap, "link"); link != "" {
			job.Link = link
		}

		jobs = append(jobs, job)
	}

	js.log("Page %d: found %d jobs", page, len(jobs))
	return jobs, nil
}

// FormatJobResults formats jobs into a readable string for piping
func FormatJobResults(jobs []JobResult) string {
	if len(jobs) == 0 {
		return "No jobs found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Job Search Results (%d jobs found)\n\n", len(jobs)))

	for i, job := range jobs {
		sb.WriteString(fmt.Sprintf("## %d. %s\n", i+1, job.Title))
		sb.WriteString(fmt.Sprintf("**Company:** %s\n", job.Company))
		sb.WriteString(fmt.Sprintf("**Location:** %s\n", job.Location))

		if job.Salary != "" {
			sb.WriteString(fmt.Sprintf("**Salary:** %s\n", job.Salary))
		}
		if job.Schedule != "" {
			sb.WriteString(fmt.Sprintf("**Type:** %s\n", job.Schedule))
		}
		if job.PostedAt != "" {
			sb.WriteString(fmt.Sprintf("**Posted:** %s\n", job.PostedAt))
		}
		if job.Via != "" {
			sb.WriteString(fmt.Sprintf("**Source:** %s\n", job.Via))
		}

		// Apply links
		if len(job.ApplyLinks) > 0 {
			sb.WriteString("**Apply:**\n")
			for _, link := range job.ApplyLinks {
				sb.WriteString(fmt.Sprintf("  - %s\n", link))
			}
		} else if job.Link != "" {
			sb.WriteString(fmt.Sprintf("**Link:** %s\n", job.Link))
		}

		// Brief description
		if job.Description != "" {
			sb.WriteString(fmt.Sprintf("\n%s\n", job.Description))
		}

		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

// FormatJobResultsTable formats jobs as a markdown table (compact)
func FormatJobResultsTable(jobs []JobResult) string {
	if len(jobs) == 0 {
		return "No jobs found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Job Search Results (%d jobs)\n\n", len(jobs)))
	sb.WriteString("| # | Title | Company | Location | Salary | Type | Posted | Source |\n")
	sb.WriteString("|---|-------|---------|----------|--------|------|--------|--------|\n")

	for i, job := range jobs {
		salary := job.Salary
		if salary == "" {
			salary = "-"
		}
		schedule := job.Schedule
		if schedule == "" {
			schedule = "-"
		}
		posted := job.PostedAt
		if posted == "" {
			posted = "-"
		}
		via := strings.TrimPrefix(job.Via, "via ")

		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s | %s | %s | %s |\n",
			i+1, job.Title, job.Company, job.Location, salary, schedule, posted, via))
	}

	// Add apply links section
	sb.WriteString("\n## Apply Links\n\n")
	for i, job := range jobs {
		if len(job.ApplyLinks) > 0 {
			sb.WriteString(fmt.Sprintf("%d. **%s** @ %s\n", i+1, job.Title, job.Company))
			for _, link := range job.ApplyLinks {
				sb.WriteString(fmt.Sprintf("   - %s\n", link))
			}
		}
	}

	return sb.String()
}

// getString safely extracts a string from a map
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
