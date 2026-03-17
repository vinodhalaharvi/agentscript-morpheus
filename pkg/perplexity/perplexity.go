// Package perplexity provides an AI-powered search client using the Perplexity API.
// Perplexity returns synthesized, cited answers — not just links.
// This makes it the perfect pipeline source: its output is already
// readable prose that Gemini/Claude can reason over directly.
package perplexity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiURL = "https://api.perplexity.ai/chat/completions"

// Model constants — sonar models are Perplexity's search-augmented models.
// sonar is fast and cheap, sonar-pro is deeper and more accurate.
const (
	ModelSonar     = "sonar"
	ModelSonarPro  = "sonar-pro"
	ModelSonarDeep = "sonar-deep-research" // long-form research
	DefaultModel   = ModelSonar
)

// Client is the Perplexity API client.
type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
	verbose    bool
}

// NewClient creates a new Perplexity client.
func NewClient(apiKey, model string, verbose bool) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		verbose:    verbose,
	}
}

// --- Request/Response types ---

type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	// Search options
	SearchDomainFilter     []string `json:"search_domain_filter,omitempty"`
	ReturnImages           bool     `json:"return_images,omitempty"`
	ReturnRelatedQuestions bool     `json:"return_related_questions,omitempty"`
	SearchRecencyFilter    string   `json:"search_recency_filter,omitempty"` // month, week, day, hour
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type response struct {
	ID        string   `json:"id"`
	Model     string   `json:"model"`
	Choices   []choice `json:"choices"`
	Citations []string `json:"citations"`
	Usage     usage    `json:"usage"`
}

type choice struct {
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SearchOptions configures a search request.
type SearchOptions struct {
	Model           string
	RecencyFilter   string   // "month", "week", "day", "hour" — empty means any time
	DomainFilter    []string // restrict to these domains
	InciteCitations bool     // append numbered citations to output
}

// Search performs an AI-powered web search and returns a synthesized answer.
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) (string, error) {
	model := opts.Model
	if model == "" {
		model = c.model
	}

	req := request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: "Be precise and concise. Always cite your sources."},
			{Role: "user", Content: query},
		},
		ReturnRelatedQuestions: false,
	}

	if opts.RecencyFilter != "" {
		req.SearchRecencyFilter = opts.RecencyFilter
	}
	if len(opts.DomainFilter) > 0 {
		req.SearchDomainFilter = opts.DomainFilter
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("perplexity: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("perplexity: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	if c.verbose {
		fmt.Printf("[perplexity] querying model=%s query=%q\n", model, query)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("perplexity: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("perplexity: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("perplexity: API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result response
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("perplexity: unmarshal response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("perplexity: empty response")
	}

	answer := result.Choices[0].Message.Content

	// Append citations if present
	if len(result.Citations) > 0 {
		var sb strings.Builder
		sb.WriteString(answer)
		sb.WriteString("\n\n**Sources:**\n")
		for i, cite := range result.Citations {
			sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, cite))
		}
		return sb.String(), nil
	}

	return answer, nil
}

// FormatResult formats a perplexity result for pipeline output.
// Just returns as-is since it's already readable prose.
func FormatResult(query, result string) string {
	return fmt.Sprintf("## Perplexity: %s\n\n%s", query, result)
}
