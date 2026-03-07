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

// NewsArticle represents a single news article
type NewsArticle struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Content     string `json:"content"`
	URL         string `json:"url"`
	ImageURL    string `json:"image"`
	Source      string `json:"source"`
	PublishedAt string `json:"publishedAt"`
}

// NewsClient handles news API calls
type NewsClient struct {
	gnewsKey string // GNews API key (optional)
	serpKey  string // SerpAPI key (fallback)
	client   *http.Client
	verbose  bool
}

// NewNewsClient creates a new news client
func NewNewsClient(gnewsKey, serpKey string, verbose bool) *NewsClient {
	return &NewsClient{
		gnewsKey: gnewsKey,
		serpKey:  serpKey,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (nc *NewsClient) log(format string, args ...any) {
	if nc.verbose {
		fmt.Printf("[NEWS] "+format+"\n", args...)
	}
}

// Search fetches news articles for a query
func (nc *NewsClient) Search(ctx context.Context, query string, maxResults int) ([]NewsArticle, error) {
	if maxResults <= 0 {
		maxResults = 10
	}

	// Try GNews first (if key available)
	if nc.gnewsKey != "" {
		articles, err := nc.searchGNews(ctx, query, maxResults)
		if err == nil && len(articles) > 0 {
			return articles, nil
		}
		nc.log("GNews failed or empty, falling back: %v", err)
	}

	// Fallback to SerpAPI Google News
	if nc.serpKey != "" {
		return nc.searchSerpAPINews(ctx, query, maxResults)
	}

	return nil, fmt.Errorf("no news API key available. Set GNEWS_API_KEY or SERPAPI_KEY")
}

// TopHeadlines fetches top headlines by category
func (nc *NewsClient) TopHeadlines(ctx context.Context, category string, maxResults int) ([]NewsArticle, error) {
	if maxResults <= 0 {
		maxResults = 10
	}

	if nc.gnewsKey != "" {
		return nc.headlinesGNews(ctx, category, maxResults)
	}

	// Fallback: search for category news
	query := category + " news today"
	return nc.Search(ctx, query, maxResults)
}

// searchGNews uses the GNews API
func (nc *NewsClient) searchGNews(ctx context.Context, query string, max int) ([]NewsArticle, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("lang", "en")
	params.Set("max", fmt.Sprintf("%d", max))
	params.Set("apikey", nc.gnewsKey)

	apiURL := "https://gnews.io/api/v4/search?" + params.Encode()
	nc.log("GNews search: %q", query)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := nc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GNews error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		TotalArticles int `json:"totalArticles"`
		Articles      []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Content     string `json:"content"`
			URL         string `json:"url"`
			Image       string `json:"image"`
			PublishedAt string `json:"publishedAt"`
			Source      struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"source"`
		} `json:"articles"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse GNews response: %w", err)
	}

	var articles []NewsArticle
	for _, a := range result.Articles {
		articles = append(articles, NewsArticle{
			Title:       a.Title,
			Description: a.Description,
			Content:     a.Content,
			URL:         a.URL,
			ImageURL:    a.Image,
			Source:      a.Source.Name,
			PublishedAt: a.PublishedAt,
		})
	}

	nc.log("GNews returned %d articles", len(articles))
	return articles, nil
}

// headlinesGNews fetches top headlines from GNews
func (nc *NewsClient) headlinesGNews(ctx context.Context, category string, max int) ([]NewsArticle, error) {
	params := url.Values{}
	params.Set("lang", "en")
	params.Set("max", fmt.Sprintf("%d", max))
	params.Set("apikey", nc.gnewsKey)

	// Valid categories: general, world, nation, business, technology, entertainment, sports, science, health
	validCategories := map[string]bool{
		"general": true, "world": true, "nation": true, "business": true,
		"technology": true, "entertainment": true, "sports": true,
		"science": true, "health": true,
	}

	cat := strings.ToLower(category)
	if cat == "tech" {
		cat = "technology"
	}
	if !validCategories[cat] {
		cat = "general"
	}
	params.Set("category", cat)

	apiURL := "https://gnews.io/api/v4/top-headlines?" + params.Encode()
	nc.log("GNews headlines: category=%q", cat)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := nc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GNews error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Articles []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Content     string `json:"content"`
			URL         string `json:"url"`
			Image       string `json:"image"`
			PublishedAt string `json:"publishedAt"`
			Source      struct {
				Name string `json:"name"`
			} `json:"source"`
		} `json:"articles"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var articles []NewsArticle
	for _, a := range result.Articles {
		articles = append(articles, NewsArticle{
			Title:       a.Title,
			Description: a.Description,
			Content:     a.Content,
			URL:         a.URL,
			ImageURL:    a.Image,
			Source:      a.Source.Name,
			PublishedAt: a.PublishedAt,
		})
	}

	return articles, nil
}

// searchSerpAPINews uses SerpAPI's Google News engine as fallback
func (nc *NewsClient) searchSerpAPINews(ctx context.Context, query string, max int) ([]NewsArticle, error) {
	params := url.Values{}
	params.Set("engine", "google_news")
	params.Set("q", query)
	params.Set("api_key", nc.serpKey)
	params.Set("gl", "us")
	params.Set("hl", "en")

	apiURL := "https://serpapi.com/search.json?" + params.Encode()
	nc.log("SerpAPI news search: %q", query)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := nc.client.Do(req)
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

	var articles []NewsArticle

	// Parse news_results
	newsResults, ok := result["news_results"].([]any)
	if !ok {
		return articles, nil
	}

	for i, item := range newsResults {
		if i >= max {
			break
		}
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		article := NewsArticle{
			Title: getStringFromMap(m, "title"),
			URL:   getStringFromMap(m, "link"),
		}

		if snippet, ok := m["snippet"].(string); ok {
			article.Description = snippet
		}

		if source, ok := m["source"].(map[string]any); ok {
			article.Source = getStringFromMap(source, "name")
		}

		if date, ok := m["date"].(string); ok {
			article.PublishedAt = date
		}

		if thumb, ok := m["thumbnail"].(string); ok {
			article.ImageURL = thumb
		}

		articles = append(articles, article)
	}

	nc.log("SerpAPI returned %d articles", len(articles))
	return articles, nil
}

// FormatNewsResults formats news articles into readable output
func FormatNewsResults(articles []NewsArticle, query string) string {
	if len(articles) == 0 {
		return fmt.Sprintf("No news found for %q", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# News: %s (%d articles)\n\n", query, len(articles)))

	for i, a := range articles {
		sb.WriteString(fmt.Sprintf("## %d. %s\n", i+1, a.Title))
		sb.WriteString(fmt.Sprintf("**Source:** %s", a.Source))
		if a.PublishedAt != "" {
			// Try to parse and format the date
			if t, err := time.Parse(time.RFC3339, a.PublishedAt); err == nil {
				sb.WriteString(fmt.Sprintf(" | **Published:** %s", t.Format("Jan 2, 2006 3:04 PM")))
			} else {
				sb.WriteString(fmt.Sprintf(" | **Published:** %s", a.PublishedAt))
			}
		}
		sb.WriteString("\n")

		if a.Description != "" {
			sb.WriteString(fmt.Sprintf("\n%s\n", a.Description))
		}

		if a.URL != "" {
			sb.WriteString(fmt.Sprintf("\n🔗 %s\n", a.URL))
		}

		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

// FormatNewsTable formats news as a compact table
func FormatNewsTable(articles []NewsArticle) string {
	if len(articles) == 0 {
		return "No news articles found."
	}

	var sb strings.Builder
	sb.WriteString("| # | Title | Source | Published | Link |\n")
	sb.WriteString("|---|-------|--------|-----------|------|\n")

	for i, a := range articles {
		title := a.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		published := a.PublishedAt
		if t, err := time.Parse(time.RFC3339, a.PublishedAt); err == nil {
			published = t.Format("Jan 2")
		}
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s | [link](%s) |\n",
			i+1, title, a.Source, published, a.URL))
	}

	return sb.String()
}

// getStringFromMap safely extracts a string from a map (for SerpAPI)
func getStringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
