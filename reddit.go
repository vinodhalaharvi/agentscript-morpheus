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

// RedditPost represents a Reddit post
type RedditPost struct {
	Title     string
	Author    string
	Subreddit string
	Score     int
	NumComms  int
	URL       string
	Permalink string
	SelfText  string
	Created   float64
	IsNSFW    bool
	Flair     string
}

// RedditClient handles Reddit API calls (public JSON, no auth needed)
type RedditClient struct {
	client  *http.Client
	verbose bool
}

// NewRedditClient creates a new Reddit client
func NewRedditClient(verbose bool) *RedditClient {
	return &RedditClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (rc *RedditClient) log(format string, args ...any) {
	if rc.verbose {
		fmt.Printf("[REDDIT] "+format+"\n", args...)
	}
}

// SearchSubreddit fetches posts from a subreddit
func (rc *RedditClient) SearchSubreddit(ctx context.Context, subreddit string, sort string, limit int) ([]RedditPost, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}

	// Clean subreddit name
	subreddit = strings.TrimPrefix(subreddit, "r/")
	subreddit = strings.TrimPrefix(subreddit, "/r/")
	subreddit = strings.TrimSpace(subreddit)

	if sort == "" {
		sort = "hot"
	}
	// Valid sorts: hot, new, top, rising
	validSorts := map[string]bool{"hot": true, "new": true, "top": true, "rising": true, "best": true}
	if !validSorts[strings.ToLower(sort)] {
		sort = "hot"
	}

	apiURL := fmt.Sprintf("https://www.reddit.com/r/%s/%s.json?limit=%d&raw_json=1",
		url.PathEscape(subreddit), sort, limit)

	rc.log("Fetching r/%s (%s, limit %d)", subreddit, sort, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	// Reddit requires a User-Agent
	req.Header.Set("User-Agent", "AgentScript/1.0 (DSL for AI agents)")

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Reddit request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("Reddit rate limited. Wait 60 seconds and retry")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Reddit error (status %d): %s", resp.StatusCode, string(body))
	}

	return rc.parseListing(body)
}

// SearchReddit searches all of Reddit for a query
func (rc *RedditClient) SearchReddit(ctx context.Context, query string, sort string, limit int) ([]RedditPost, error) {
	if limit <= 0 {
		limit = 10
	}
	if sort == "" {
		sort = "relevance"
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("sort", sort)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("raw_json", "1")

	apiURL := "https://www.reddit.com/search.json?" + params.Encode()

	rc.log("Searching Reddit: %q (sort=%s)", query, sort)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AgentScript/1.0 (DSL for AI agents)")

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Reddit search error (status %d)", resp.StatusCode)
	}

	return rc.parseListing(body)
}

// parseListing parses a Reddit listing JSON response
func (rc *RedditClient) parseListing(body []byte) ([]RedditPost, error) {
	var listing struct {
		Data struct {
			Children []struct {
				Data struct {
					Title       string  `json:"title"`
					Author      string  `json:"author"`
					Subreddit   string  `json:"subreddit"`
					Score       int     `json:"score"`
					NumComments int     `json:"num_comments"`
					URL         string  `json:"url"`
					Permalink   string  `json:"permalink"`
					SelfText    string  `json:"selftext"`
					Created     float64 `json:"created_utc"`
					Over18      bool    `json:"over_18"`
					LinkFlair   string  `json:"link_flair_text"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("failed to parse Reddit response: %w", err)
	}

	var posts []RedditPost
	for _, child := range listing.Data.Children {
		d := child.Data

		// Skip NSFW
		if d.Over18 {
			continue
		}

		selfText := d.SelfText
		if len(selfText) > 300 {
			selfText = selfText[:300] + "..."
		}

		posts = append(posts, RedditPost{
			Title:     d.Title,
			Author:    d.Author,
			Subreddit: d.Subreddit,
			Score:     d.Score,
			NumComms:  d.NumComments,
			URL:       d.URL,
			Permalink: "https://reddit.com" + d.Permalink,
			SelfText:  selfText,
			Created:   d.Created,
			IsNSFW:    d.Over18,
			Flair:     d.LinkFlair,
		})
	}

	rc.log("Got %d posts", len(posts))
	return posts, nil
}

// ParseRedditArgs parses the DSL arguments
// reddit "golang"              -> search Reddit for "golang"
// reddit "r/golang"            -> browse r/golang hot posts
// reddit "r/golang" "new"      -> browse r/golang new posts
func ParseRedditArgs(args ...string) (isSubreddit bool, query string, sort string) {
	if len(args) == 0 {
		return false, "", "hot"
	}

	query = args[0]
	if len(args) >= 2 {
		sort = args[1]
	}

	// Check if it's a subreddit reference
	if strings.HasPrefix(query, "r/") || strings.HasPrefix(query, "/r/") {
		return true, query, sort
	}

	// Check if it looks like a single-word subreddit name (no spaces)
	if !strings.Contains(query, " ") && len(query) <= 25 {
		// Could be either a subreddit or search term — default to search
		// User can force subreddit with "r/" prefix
	}

	return false, query, sort
}

// FormatRedditPosts formats Reddit posts into readable output
func FormatRedditPosts(posts []RedditPost, query string) string {
	if len(posts) == 0 {
		return fmt.Sprintf("No Reddit posts found for %q", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Reddit: %s (%d posts)\n\n", query, len(posts)))

	for i, p := range posts {
		// Time ago
		timeAgo := timeAgoStr(p.Created)

		flair := ""
		if p.Flair != "" {
			flair = fmt.Sprintf(" [%s]", p.Flair)
		}

		sb.WriteString(fmt.Sprintf("## %d. %s%s\n", i+1, p.Title, flair))
		sb.WriteString(fmt.Sprintf("**r/%s** | ⬆ %d | 💬 %d | by u/%s | %s\n",
			p.Subreddit, p.Score, p.NumComms, p.Author, timeAgo))

		if p.SelfText != "" {
			sb.WriteString(fmt.Sprintf("\n> %s\n", p.SelfText))
		}

		sb.WriteString(fmt.Sprintf("\n🔗 %s\n", p.Permalink))
		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

// FormatRedditTable formats posts as a compact table
func FormatRedditTable(posts []RedditPost) string {
	if len(posts) == 0 {
		return "No posts found."
	}

	var sb strings.Builder
	sb.WriteString("| # | Title | Subreddit | Score | Comments | Age |\n")
	sb.WriteString("|---|-------|-----------|-------|----------|-----|\n")

	for i, p := range posts {
		title := p.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("| %d | %s | r/%s | %d | %d | %s |\n",
			i+1, title, p.Subreddit, p.Score, p.NumComms, timeAgoStr(p.Created)))
	}

	return sb.String()
}

// timeAgoStr converts a Unix timestamp to "X hours ago" format
func timeAgoStr(unixTime float64) string {
	t := time.Unix(int64(unixTime), 0)
	diff := time.Since(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
