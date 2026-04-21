package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClaudeClient handles Anthropic Claude API
type ClaudeClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewClaudeClient creates a new Claude API client
func NewClaudeClient(apiKey string) *ClaudeClient {
	model := "claude-opus-4-7"
	return &ClaudeClient{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
	}
}

// Message represents a Claude message
type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GenerateReactSPA generates a React SPA using Claude
func (c *ClaudeClient) GenerateReactSPA(ctx context.Context, title, content string) (string, error) {
	prompt := fmt.Sprintf(`Generate a beautiful, modern React single-page application (SPA) for the following content.

TITLE: %s

CONTENT:
%s

REQUIREMENTS:
1. Output ONLY the complete HTML file with embedded React (using babel standalone from cdnjs)
2. Use React hooks (useState, useEffect) 
3. Modern, dark theme UI with beautiful gradients and subtle animations
4. Responsive design with Tailwind CSS (via CDN)
5. Include smooth scroll animations and transitions
6. Add a sticky navigation header if content has multiple sections
7. Use emojis strategically for visual appeal
8. Make it visually stunning - this is for a hackathon demo!
9. Include a footer with "Built with AgentScript + Claude"
10. Add a subtle particle or gradient animation in the background
11. Use CSS variables for theming
12. Include proper meta tags for SEO

TECHNICAL REQUIREMENTS:
- Use https://unpkg.com/react@18/umd/react.production.min.js
- Use https://unpkg.com/react-dom@18/umd/react-dom.production.min.js  
- Use https://unpkg.com/@babel/standalone/babel.min.js
- Use https://cdn.tailwindcss.com for Tailwind
- Script type must be "text/babel" for JSX

OUTPUT FORMAT:
Return ONLY the HTML code starting with <!DOCTYPE html> and ending with </html>
No markdown code fences, no explanation, just the raw HTML.`, title, content)

	reqBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 32000,
		"messages": []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Claude API error: status %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if claudeResp.Error != nil {
		return "", fmt.Errorf("Claude error: %s", claudeResp.Error.Message)
	}

	if len(claudeResp.Content) == 0 {
		return "", fmt.Errorf("no content in Claude response")
	}

	result := claudeResp.Content[0].Text

	// Clean up response
	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```html")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	// Validate HTML
	if !strings.HasPrefix(result, "<!DOCTYPE html>") && !strings.HasPrefix(result, "<html") {
		if idx := strings.Index(result, "<!DOCTYPE html>"); idx != -1 {
			result = result[idx:]
		} else if idx := strings.Index(result, "<html"); idx != -1 {
			result = result[idx:]
		} else {
			return "", fmt.Errorf("Claude did not return valid HTML")
		}
	}

	return result, nil
}

// Chat sends a message to Claude and returns the response
func (c *ClaudeClient) Chat(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 32000,
		"messages": []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}
	return c.doRequest(ctx, reqBody)
}

// Session maintains a multi-turn conversation with Claude
type Session struct {
	client       *ClaudeClient
	messages     []claudeMessage
	SystemPrompt string // persistent system prompt — never dropped
	TotalInput   int    // cumulative input tokens
	TotalOutput  int    // cumulative output tokens
	CallCount    int    // number of API calls
	LastInput    int    // last call input tokens
	LastOutput   int    // last call output tokens
}

// NewSession creates a new conversational session
func (c *ClaudeClient) NewSession() *Session {
	return &Session{
		client:   c,
		messages: make([]claudeMessage, 0),
	}
}

// Chat sends a message in the session and returns the response.
// Maintains conversation history — Claude remembers previous turns.
func (s *Session) Chat(ctx context.Context, prompt string) (string, error) {
	// Add user message to history
	s.messages = append(s.messages, claudeMessage{Role: "user", Content: prompt})

	reqBody := map[string]interface{}{
		"model":      s.client.model,
		"max_tokens": 32000,
		"messages":   s.messages,
	}

	// System prompt is always sent — never dropped from context
	if s.SystemPrompt != "" {
		reqBody["system"] = s.SystemPrompt
	}

	response, usage, err := s.client.doRequestWithUsage(ctx, reqBody)
	if err != nil {
		// Remove the failed user message
		s.messages = s.messages[:len(s.messages)-1]
		return "", err
	}

	// Track tokens
	s.CallCount++
	if usage != nil {
		s.LastInput = usage.InputTokens
		s.LastOutput = usage.OutputTokens
		s.TotalInput += usage.InputTokens
		s.TotalOutput += usage.OutputTokens
	}

	// Add assistant response to history
	s.messages = append(s.messages, claudeMessage{Role: "assistant", Content: response})

	return response, nil
}

// MessageCount returns the number of messages in the session
func (s *Session) MessageCount() int {
	return len(s.messages)
}

// TokenSummary returns a formatted string of token usage
func (s *Session) TokenSummary() string {
	return fmt.Sprintf("call %d: %d in / %d out (total: %d in / %d out)",
		s.CallCount, s.LastInput, s.LastOutput, s.TotalInput, s.TotalOutput)
}

// TokenUsage tracks tokens consumed per API call
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// doRequest makes the actual API call and returns response + token usage
func (c *ClaudeClient) doRequest(ctx context.Context, reqBody map[string]interface{}) (string, error) {
	text, _, err := c.doRequestWithUsage(ctx, reqBody)
	return text, err
}

func (c *ClaudeClient) doRequestWithUsage(ctx context.Context, reqBody map[string]interface{}) (string, *TokenUsage, error) {

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("Claude API error: status %d - %s", resp.StatusCode, string(body))
	}

	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage *TokenUsage `json:"usage"`
	}

	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return "", nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		return "", nil, fmt.Errorf("no content in response")
	}

	return claudeResp.Content[0].Text, claudeResp.Usage, nil
}
