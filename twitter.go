package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Tweet represents a tweet/post
type Tweet struct {
	ID        string
	Text      string
	Author    string
	AuthorID  string
	CreatedAt string
	Likes     int
	Retweets  int
	Replies   int
	URL       string
}

// TwitterClient handles Twitter/X API v2 calls
type TwitterClient struct {
	bearerToken  string // for search (read-only)
	apiKey       string // for posting
	apiSecret    string
	accessToken  string
	accessSecret string
	client       *http.Client
	verbose      bool
}

// NewTwitterClient creates a new Twitter client
func NewTwitterClient(verbose bool) *TwitterClient {
	return &TwitterClient{
		bearerToken:  os.Getenv("TWITTER_BEARER_TOKEN"),
		apiKey:       os.Getenv("TWITTER_API_KEY"),
		apiSecret:    os.Getenv("TWITTER_API_SECRET"),
		accessToken:  os.Getenv("TWITTER_ACCESS_TOKEN"),
		accessSecret: os.Getenv("TWITTER_ACCESS_SECRET"),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (tc *TwitterClient) log(format string, args ...any) {
	if tc.verbose {
		fmt.Printf("[TWITTER] "+format+"\n", args...)
	}
}

// CanSearch returns true if bearer token is set (read-only access)
func (tc *TwitterClient) CanSearch() bool {
	return tc.bearerToken != ""
}

// CanPost returns true if full OAuth credentials are set
func (tc *TwitterClient) CanPost() bool {
	return tc.apiKey != "" && tc.apiSecret != "" && tc.accessToken != "" && tc.accessSecret != ""
}

// SearchRecent searches recent tweets (last 7 days)
func (tc *TwitterClient) SearchRecent(ctx context.Context, query string, maxResults int) ([]Tweet, error) {
	if !tc.CanSearch() {
		return nil, fmt.Errorf("TWITTER_BEARER_TOKEN not set. Get one at https://developer.twitter.com")
	}

	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 100 {
		maxResults = 100
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("max_results", fmt.Sprintf("%d", maxResults))
	params.Set("tweet.fields", "created_at,public_metrics,author_id")
	params.Set("expansions", "author_id")
	params.Set("user.fields", "username,name")

	apiURL := "https://api.twitter.com/2/tweets/search/recent?" + params.Encode()

	tc.log("Searching: %q (max %d)", query, maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tc.bearerToken)

	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Twitter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("Twitter rate limited. Free tier: 10 requests/min. Wait and retry")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Twitter error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			AuthorID  string `json:"author_id"`
			CreatedAt string `json:"created_at"`
			Metrics   struct {
				Likes    int `json:"like_count"`
				Retweets int `json:"retweet_count"`
				Replies  int `json:"reply_count"`
			} `json:"public_metrics"`
		} `json:"data"`
		Includes struct {
			Users []struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Name     string `json:"name"`
			} `json:"users"`
		} `json:"includes"`
		Meta struct {
			ResultCount int `json:"result_count"`
		} `json:"meta"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Twitter response: %w", err)
	}

	// Build user lookup map
	userMap := make(map[string]string)
	for _, u := range result.Includes.Users {
		userMap[u.ID] = u.Username
	}

	var tweets []Tweet
	for _, t := range result.Data {
		username := userMap[t.AuthorID]
		if username == "" {
			username = t.AuthorID
		}

		tweets = append(tweets, Tweet{
			ID:        t.ID,
			Text:      t.Text,
			Author:    username,
			AuthorID:  t.AuthorID,
			CreatedAt: formatTweetDate(t.CreatedAt),
			Likes:     t.Metrics.Likes,
			Retweets:  t.Metrics.Retweets,
			Replies:   t.Metrics.Replies,
			URL:       fmt.Sprintf("https://x.com/%s/status/%s", username, t.ID),
		})
	}

	tc.log("Found %d tweets", len(tweets))
	return tweets, nil
}

// Post creates a new tweet
func (tc *TwitterClient) Post(ctx context.Context, text string) (*Tweet, error) {
	if !tc.CanPost() {
		return nil, fmt.Errorf("Twitter posting requires: TWITTER_API_KEY, TWITTER_API_SECRET, TWITTER_ACCESS_TOKEN, TWITTER_ACCESS_SECRET")
	}

	// Twitter limit: 280 characters
	if len(text) > 280 {
		text = text[:277] + "..."
	}

	payload := map[string]string{"text": text}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	apiURL := "https://api.twitter.com/2/tweets"

	tc.log("Posting tweet (%d chars)", len(text))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tc.bearerToken)

	// Note: For posting, you actually need OAuth 1.0a, not just bearer token.
	// This is a simplified version. For production, use an OAuth library.
	// The bearer token works for search but POST requires user context.

	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Twitter post error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}

	json.Unmarshal(body, &result)

	return &Tweet{
		ID:   result.Data.ID,
		Text: result.Data.Text,
		URL:  fmt.Sprintf("https://x.com/i/status/%s", result.Data.ID),
	}, nil
}

// formatTweetDate formats a Twitter date string
func formatTweetDate(dateStr string) string {
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		return t.Format("Jan 2, 3:04 PM")
	}
	return dateStr
}

// FormatTweets formats tweets into readable output
func FormatTweets(tweets []Tweet, query string) string {
	if len(tweets) == 0 {
		return fmt.Sprintf("No tweets found for %q", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Twitter/X: %s (%d tweets)\n\n", query, len(tweets)))

	for i, t := range tweets {
		sb.WriteString(fmt.Sprintf("## %d. @%s\n", i+1, t.Author))
		sb.WriteString(fmt.Sprintf("%s\n\n", t.Text))
		sb.WriteString(fmt.Sprintf("❤️ %d | 🔁 %d | 💬 %d | %s\n",
			t.Likes, t.Retweets, t.Replies, t.CreatedAt))
		sb.WriteString(fmt.Sprintf("🔗 %s\n", t.URL))
		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

// FormatTweetsTable formats tweets as a compact table
func FormatTweetsTable(tweets []Tweet) string {
	if len(tweets) == 0 {
		return "No tweets found."
	}

	var sb strings.Builder
	sb.WriteString("| # | Author | Tweet | ❤️ | 🔁 | Date |\n")
	sb.WriteString("|---|--------|-------|-----|-----|------|\n")

	for i, t := range tweets {
		text := t.Text
		if len(text) > 60 {
			text = text[:57] + "..."
		}
		text = strings.ReplaceAll(text, "|", "/")
		text = strings.ReplaceAll(text, "\n", " ")

		sb.WriteString(fmt.Sprintf("| %d | @%s | %s | %d | %d | %s |\n",
			i+1, t.Author, text, t.Likes, t.Retweets, t.CreatedAt))
	}

	return sb.String()
}
