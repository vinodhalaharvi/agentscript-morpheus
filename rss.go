package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RSSItem represents a single feed item
type RSSItem struct {
	Title       string
	Link        string
	Description string
	Author      string
	PubDate     string
	Source      string
}

// RSSClient handles RSS feed fetching
type RSSClient struct {
	client  *http.Client
	verbose bool
}

// NewRSSClient creates a new RSS client
func NewRSSClient(verbose bool) *RSSClient {
	return &RSSClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (rc *RSSClient) log(format string, args ...any) {
	if rc.verbose {
		fmt.Printf("[RSS] "+format+"\n", args...)
	}
}

// Well-known feed shortcuts
var feedShortcuts = map[string]string{
	"hn":            "https://news.ycombinator.com/rss",
	"hackernews":    "https://news.ycombinator.com/rss",
	"lobsters":      "https://lobste.rs/rss",
	"golang":        "https://go.dev/blog/feed.atom",
	"go-blog":       "https://go.dev/blog/feed.atom",
	"kubernetes":    "https://kubernetes.io/feed.xml",
	"k8s":           "https://kubernetes.io/feed.xml",
	"techcrunch":    "https://techcrunch.com/feed/",
	"arstechnica":   "https://feeds.arstechnica.com/arstechnica/index",
	"verge":         "https://www.theverge.com/rss/index.xml",
	"wired":         "https://www.wired.com/feed/rss",
	"bbc":           "http://feeds.bbci.co.uk/news/rss.xml",
	"bbc-tech":      "http://feeds.bbci.co.uk/news/technology/rss.xml",
	"reuters":       "https://www.reutersagency.com/feed/",
	"cncf":          "https://www.cncf.io/feed/",
	"docker":        "https://www.docker.com/blog/feed/",
	"github-blog":   "https://github.blog/feed/",
	"aws":           "https://aws.amazon.com/blogs/aws/feed/",
	"google-ai":     "https://blog.google/technology/ai/rss/",
	"anthropic":     "https://www.anthropic.com/feed.xml",
	"openai":        "https://openai.com/blog/rss.xml",
	"reddit-golang": "https://www.reddit.com/r/golang/.rss",
	"reddit-devops": "https://www.reddit.com/r/devops/.rss",
	"reddit-k8s":    "https://www.reddit.com/r/kubernetes/.rss",
	"reddit-sre":    "https://www.reddit.com/r/sre/.rss",
	"reddit-netsec": "https://www.reddit.com/r/netsec/.rss",
	"producthunt":   "https://www.producthunt.com/feed",
	"dev-to":        "https://dev.to/feed",
	"medium-golang": "https://medium.com/feed/tag/golang",
	"morning-brew":  "https://www.morningbrew.com/feed",
}

// resolveFeedURL converts a shortcut or URL to a full feed URL
func resolveFeedURL(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))

	// Check shortcuts
	if url, ok := feedShortcuts[lower]; ok {
		return url
	}

	// If it looks like a URL, use as-is
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return input
	}

	// Try adding https:// and /feed or /rss
	return "https://" + input + "/feed"
}

// FetchFeed fetches and parses an RSS or Atom feed
func (rc *RSSClient) FetchFeed(ctx context.Context, feedURL string, maxItems int) ([]RSSItem, string, error) {
	if maxItems <= 0 {
		maxItems = 10
	}

	resolvedURL := resolveFeedURL(feedURL)
	rc.log("Fetching feed: %s", resolvedURL)

	req, err := http.NewRequestWithContext(ctx, "GET", resolvedURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "AgentScript/1.0 (DSL for AI agents)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("feed fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("feed error (status %d): %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	// Try RSS 2.0 first
	items, title, err := rc.parseRSS(body, maxItems)
	if err == nil && len(items) > 0 {
		return items, title, nil
	}

	// Try Atom
	items, title, err = rc.parseAtom(body, maxItems)
	if err == nil && len(items) > 0 {
		return items, title, nil
	}

	return nil, "", fmt.Errorf("could not parse feed from %s (not valid RSS or Atom)", resolvedURL)
}

// RSS 2.0 structures
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Author      string `xml:"author"`
	Creator     string `xml:"creator"`
	PubDate     string `xml:"pubDate"`
}

// Atom structures
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Link    []atomLink `xml:"link"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func (rc *RSSClient) parseRSS(body []byte, maxItems int) ([]RSSItem, string, error) {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, "", err
	}

	if len(feed.Channel.Items) == 0 {
		return nil, "", fmt.Errorf("no RSS items found")
	}

	var items []RSSItem
	for i, item := range feed.Channel.Items {
		if i >= maxItems {
			break
		}

		author := item.Author
		if author == "" {
			author = item.Creator
		}

		desc := stripHTML(item.Description)
		if len(desc) > 300 {
			desc = desc[:300] + "..."
		}

		items = append(items, RSSItem{
			Title:       item.Title,
			Link:        item.Link,
			Description: desc,
			Author:      author,
			PubDate:     formatPubDate(item.PubDate),
			Source:      feed.Channel.Title,
		})
	}

	return items, feed.Channel.Title, nil
}

func (rc *RSSClient) parseAtom(body []byte, maxItems int) ([]RSSItem, string, error) {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, "", err
	}

	if len(feed.Entries) == 0 {
		return nil, "", fmt.Errorf("no Atom entries found")
	}

	var items []RSSItem
	for i, entry := range feed.Entries {
		if i >= maxItems {
			break
		}

		link := ""
		for _, l := range entry.Link {
			if l.Rel == "alternate" || l.Rel == "" {
				link = l.Href
				break
			}
		}
		if link == "" && len(entry.Link) > 0 {
			link = entry.Link[0].Href
		}

		desc := entry.Summary
		if desc == "" {
			desc = entry.Content
		}
		desc = stripHTML(desc)
		if len(desc) > 300 {
			desc = desc[:300] + "..."
		}

		pubDate := entry.Published
		if pubDate == "" {
			pubDate = entry.Updated
		}

		items = append(items, RSSItem{
			Title:       entry.Title,
			Link:        link,
			Description: desc,
			Author:      entry.Author.Name,
			PubDate:     formatPubDate(pubDate),
			Source:      feed.Title,
		})
	}

	return items, feed.Title, nil
}

// stripHTML removes HTML tags from a string (simple version)
func stripHTML(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	// Clean up whitespace
	return strings.Join(strings.Fields(result.String()), " ")
}

// formatPubDate tries to make dates human-readable
func formatPubDate(dateStr string) string {
	if dateStr == "" {
		return ""
	}

	// Try common formats
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, f := range formats {
		if t, err := time.Parse(f, dateStr); err == nil {
			return t.Format("Jan 2, 2006")
		}
	}

	return dateStr
}

// ListFeedShortcuts returns available shortcuts
func ListFeedShortcuts() string {
	var sb strings.Builder
	sb.WriteString("# Available RSS Feed Shortcuts\n\n")
	sb.WriteString("| Shortcut | Feed |\n")
	sb.WriteString("|----------|------|\n")

	// Group by category
	categories := map[string][]string{
		"Tech News": {"hn", "lobsters", "techcrunch", "arstechnica", "verge", "wired", "producthunt", "dev-to"},
		"Go/DevOps": {"golang", "kubernetes", "docker", "cncf", "reddit-golang", "reddit-devops", "reddit-k8s"},
		"AI/ML":     {"google-ai", "anthropic", "openai"},
		"General":   {"bbc", "bbc-tech", "reuters", "morning-brew"},
		"Platforms": {"github-blog", "aws"},
	}

	for cat, shortcuts := range categories {
		sb.WriteString(fmt.Sprintf("| **%s** | |\n", cat))
		for _, s := range shortcuts {
			if url, ok := feedShortcuts[s]; ok {
				sb.WriteString(fmt.Sprintf("| `%s` | %s |\n", s, url))
			}
		}
	}

	return sb.String()
}

// FormatRSSItems formats feed items into readable output
func FormatRSSItems(items []RSSItem, feedTitle string) string {
	if len(items) == 0 {
		return "No feed items found."
	}

	var sb strings.Builder
	title := feedTitle
	if title == "" {
		title = "RSS Feed"
	}
	sb.WriteString(fmt.Sprintf("# %s (%d items)\n\n", title, len(items)))

	for i, item := range items {
		sb.WriteString(fmt.Sprintf("## %d. %s\n", i+1, item.Title))

		meta := []string{}
		if item.Source != "" {
			meta = append(meta, fmt.Sprintf("**Source:** %s", item.Source))
		}
		if item.Author != "" {
			meta = append(meta, fmt.Sprintf("**By:** %s", item.Author))
		}
		if item.PubDate != "" {
			meta = append(meta, fmt.Sprintf("**Date:** %s", item.PubDate))
		}
		if len(meta) > 0 {
			sb.WriteString(strings.Join(meta, " | ") + "\n")
		}

		if item.Description != "" {
			sb.WriteString(fmt.Sprintf("\n%s\n", item.Description))
		}

		if item.Link != "" {
			sb.WriteString(fmt.Sprintf("\n🔗 %s\n", item.Link))
		}

		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

// min is provided by Go 1.22+ builtin
