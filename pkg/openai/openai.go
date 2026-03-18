// Package openai provides a minimal OpenAI API client.
// Same pattern as pkg/claude — Chat(ctx, prompt) (string, error).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiURL = "https://api.openai.com/v1/chat/completions"

// Model constants
const (
	ModelGPT4o     = "gpt-4o"
	ModelGPT4oMini = "gpt-4o-mini"
	ModelO1        = "o1"
	DefaultModel   = ModelGPT4o
)

// Client is the OpenAI API client.
type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewClient creates a new OpenAI client.
func NewClient(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type response struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends a prompt and returns the response.
// Signature matches pkg/claude.Chat and pkg/gemini.GenerateContent —
// all three are Reviewer-compatible.
func (c *Client) Chat(ctx context.Context, prompt string) (string, error) {
	reqBody := request{
		Model: c.model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result response
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("openai: unmarshal: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("openai: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}

	return result.Choices[0].Message.Content, nil
}
