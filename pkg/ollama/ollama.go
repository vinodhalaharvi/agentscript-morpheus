package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Client talks to a local Ollama server.
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewClient creates an Ollama client.
// baseURL defaults to http://localhost:11434 if empty.
// model defaults to OLLAMA_MODEL env or "mistral" if empty.
func NewClient(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = os.Getenv("OLLAMA_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if model == "" {
		model = os.Getenv("OLLAMA_MODEL")
	}
	if model == "" {
		model = "mistral"
	}

	return &Client{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Chat sends a prompt to the local Ollama server and returns the response.
// This is the Reasoner seam — same signature as claude.Chat and gemini.GenerateContent.
func (c *Client) Chat(ctx context.Context, prompt string) (string, error) {
	return c.Generate(ctx, c.model, prompt)
}

// Generate calls the Ollama /api/generate endpoint.
func (c *Client) Generate(ctx context.Context, model, prompt string) (string, error) {
	if model == "" {
		model = c.model
	}

	reqBody := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("ollama: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: request failed (is Ollama running at %s?): %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: server returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("ollama: failed to parse response: %w", err)
	}

	return strings.TrimSpace(result.Response), nil
}

// Plugin exposes the ollama command to the AgentScript DSL.
type Plugin struct {
	client  *Client
	verbose bool
}

// NewPlugin creates the Ollama plugin.
func NewPlugin(client *Client, verbose bool) *Plugin {
	return &Plugin{client: client, verbose: verbose}
}

func (p *Plugin) Name() string { return "ollama" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"ollama": p.ollama,
	}
}

func (p *Plugin) ollama(ctx context.Context, args []string, input string) (string, error) {
	prompt := plugin.Coalesce(args, 0, "")
	model := plugin.Arg(args, 1) // optional model override

	if prompt == "" && input == "" {
		return "", fmt.Errorf("ollama requires a prompt\n\n  Usage:\n    ollama \"summarize this\"\n    read \"data.txt\" >=> ollama \"analyze this data\"\n    read \"data.txt\" >=> ollama \"analyze\" \"nemotron-3-super\"")
	}

	// Build full prompt with piped input
	fullPrompt := prompt
	if input != "" {
		if prompt != "" {
			fullPrompt = prompt + "\n\nContext:\n" + input
		} else {
			fullPrompt = input
		}
	}

	if p.verbose {
		m := model
		if m == "" {
			m = p.client.model
		}
		fmt.Fprintf(os.Stderr, "🦙 ollama [%s]: processing (%d chars)...\n", m, len(fullPrompt))
	}

	result, err := p.client.Generate(ctx, model, fullPrompt)
	if err != nil {
		return "", fmt.Errorf("ollama failed: %w", err)
	}

	if p.verbose {
		fmt.Fprintf(os.Stderr, "✅ ollama: done (%d chars)\n", len(result))
	}

	return result, nil
}
