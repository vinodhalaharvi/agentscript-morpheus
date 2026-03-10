package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

// TestGeminiToolCall verifies that Gemini REST API correctly calls agentscript_dsl
// when asked a question that requires data lookup.
// Run with: GEMINI_API_KEY=xxx go test ./cmd/geminilive/ -run TestGeminiToolCall -v
func TestGeminiToolCall(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	queries := []struct {
		prompt     string
		expectTool bool
	}{
		{"What is the weather in New York?", true},
		{"What is the Bitcoin price?", true},
		{"Find me golang developer jobs in San Francisco", true},
		{"What is 2 + 2?", false}, // should NOT call tool
	}

	for _, q := range queries {
		t.Run(q.prompt, func(t *testing.T) {
			dsl, calledTool, err := callGeminiREST(key, q.prompt)
			if err != nil {
				t.Fatalf("API error: %v", err)
			}
			if q.expectTool && !calledTool {
				t.Errorf("expected tool call but got none. Response: %s", dsl)
			}
			if !q.expectTool && calledTool {
				t.Errorf("expected no tool call but got DSL: %s", dsl)
			}
			if calledTool {
				t.Logf("✅ DSL: %s", dsl)
			} else {
				t.Logf("💬 Direct answer (no tool call)")
			}
		})
	}
}

func callGeminiREST(apiKey, prompt string) (dsl string, calledTool bool, err error) {
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=%s",
		apiKey,
	)

	body := map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": prompt}},
			},
		},
		"tools": []any{
			map[string]any{
				"function_declarations": []any{
					map[string]any{
						"name":        "agentscript_dsl",
						"description": `Execute AgentScript DSL. Syntax: command "arg". No dots, no parens, no equals.`,
						"parameters": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"dsl": map[string]any{
									"type":        "string",
									"description": `DSL string e.g.: weather "NYC" or job_search "golang" "remote" "fulltime"`,
								},
							},
							"required": []string{"dsl"},
						},
					},
				},
			},
		},
		"tool_config": map[string]any{
			"function_calling_config": map[string]any{
				"mode": "AUTO",
			},
		},
		"system_instruction": map[string]any{
			"parts": []any{map[string]any{"text": MorpheusAgentSystemPrompt}},
		},
	}

	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(raw, &result); err != nil {
		return "", false, fmt.Errorf("parse response: %w\nraw: %s", err, raw)
	}
	if result.Error != nil {
		return "", false, fmt.Errorf("gemini error: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 {
		return "", false, fmt.Errorf("no candidates in response: %s", raw)
	}

	for _, part := range result.Candidates[0].Content.Parts {
		if part.FunctionCall != nil && part.FunctionCall.Name == "agentscript_dsl" {
			if d, ok := part.FunctionCall.Args["dsl"].(string); ok {
				return d, true, nil
			}
		}
	}
	return "", false, nil
}
