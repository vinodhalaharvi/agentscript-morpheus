// Package mcpagent provides AI-driven MCP tool selection.
//
// This is the bridge between your DSL and Claude Desktop parity.
// Instead of manually writing JSON args for mcp "server:tool" '{...}',
// you describe your intent in plain English and the AI picks the right
// tool and generates the correct arguments.
//
// Seams in play:
//
//	Reasoner   = func(ctx, prompt) (string, error) — Claude or Gemini decides
//	MCPClient  = injected from runtime (already connected servers)
//
// Same pattern as every plugin before it.
package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/mcp"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Reasoner is the AI seam — Claude or Gemini deciding which tool to call
// and what arguments to pass. Same signature as Reviewer in pkg/review.
type Reasoner func(ctx context.Context, prompt string) (string, error)

// Plugin is the mcp_agent plugin.
type Plugin struct {
	mcp      *mcp.MCPClient
	reasoner Reasoner
	verbose  bool
}

// NewPlugin creates an mcpagent plugin.
// mcp:      shared MCPClient from the runtime (already has connected servers)
// reasoner: Claude.Chat or Gemini.GenerateContent injected from registry
func NewPlugin(mcpClient *mcp.MCPClient, reasoner Reasoner, verbose bool) *Plugin {
	return &Plugin{
		mcp:      mcpClient,
		reasoner: reasoner,
		verbose:  verbose,
	}
}

func (p *Plugin) Name() string { return "mcpagent" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		// mcp_agent "server" "intent"
		// e.g. mcp_agent "github" "create an issue about the memory leak in agent.go"
		"mcp_agent": p.mcpAgent,
	}
}

// mcpAgent is the main command.
//
// Usage:
//
//	mcp_connect "github" "npx -y @modelcontextprotocol/server-github"
//	>=> mcp_agent "github" "create an issue about the memory leak in agent.go"
//
// Flow:
//  1. List all tools from the connected MCP server
//  2. Send tool schemas + user intent + piped context to the Reasoner (Claude/Gemini)
//  3. Parse the AI's decision: tool name + JSON arguments
//  4. Call the tool via MCPClient
//  5. Return the result
func (p *Plugin) mcpAgent(ctx context.Context, args []string, input string) (string, error) {
	server, err := plugin.RequireArg(args, 0, "server name")
	if err != nil {
		return "", fmt.Errorf("mcp_agent: %w — usage: mcp_agent \"server\" \"intent\"", err)
	}
	intent, err := plugin.RequireArg(args, 1, "intent")
	if err != nil {
		return "", fmt.Errorf("mcp_agent: %w — usage: mcp_agent \"github\" \"create an issue about X\"", err)
	}

	// Step 1: Get available tools from the connected server
	tools, err := p.mcp.ListTools(server)
	if err != nil {
		return "", fmt.Errorf("mcp_agent: cannot list tools from %q: %w\nDid you run mcp_connect first?", server, err)
	}
	if len(tools) == 0 {
		return "", fmt.Errorf("mcp_agent: no tools available on server %q", server)
	}

	if p.verbose {
		fmt.Printf("[mcp_agent] %d tools available on %q\n", len(tools), server)
	}

	// Step 2: Ask the AI to pick the right tool + generate args
	fmt.Printf("🤖 mcp_agent: selecting tool from %d available on %q...\n", len(tools), server)

	prompt := buildToolSelectionPrompt(server, intent, input, tools)
	decision, err := p.reasoner(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("mcp_agent: AI tool selection failed: %w", err)
	}

	if p.verbose {
		fmt.Printf("[mcp_agent] AI decision:\n%s\n", decision)
	}

	// Step 3: Parse the AI's response
	toolName, argsJSON, err := parseDecision(decision)
	if err != nil {
		return "", fmt.Errorf("mcp_agent: could not parse AI response: %w\nResponse was:\n%s", err, decision)
	}

	fmt.Printf("🔧 mcp_agent: calling %s.%s\n", server, toolName)
	if p.verbose {
		fmt.Printf("[mcp_agent] args: %s\n", argsJSON)
	}

	// Step 4: Execute the tool
	result, err := p.mcp.CallTool(ctx, server, toolName, argsJSON)
	if err != nil {
		return "", fmt.Errorf("mcp_agent: tool call failed: %w", err)
	}

	fmt.Printf("✅ mcp_agent: done\n")
	return result, nil
}

// buildToolSelectionPrompt creates the prompt that tells the AI about
// the available tools and asks it to select the right one.
func buildToolSelectionPrompt(server, intent, context string, tools []mcp.MCPTool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`You are helping select and call the right MCP tool on the "%s" server.

## Available Tools

`, server))

	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("### %s\n", t.Name))
		sb.WriteString(fmt.Sprintf("Description: %s\n", t.Description))

		if t.InputSchema != nil {
			schemaJSON, _ := json.MarshalIndent(t.InputSchema, "", "  ")
			sb.WriteString(fmt.Sprintf("Input schema:\n```json\n%s\n```\n", string(schemaJSON)))
		}
		sb.WriteString("\n")
	}

	if context != "" {
		sb.WriteString(fmt.Sprintf(`## Context (piped from previous pipeline stage)

%s

`, context))
	}

	sb.WriteString(fmt.Sprintf(`## Intent

%s

## Your Task

Select the best tool and generate the correct JSON arguments.

Respond with EXACTLY this format — nothing else:
TOOL: <tool_name>
ARGS: <valid_json_object>

Example:
TOOL: create_issue
ARGS: {"owner": "vinodhalaharvi", "repo": "agentscript-morpheus", "title": "Memory leak in agent.go", "body": "Detected via code review"}

Rules:
- ARGS must be valid JSON
- Only include fields that are in the tool's input schema
- Use context from the pipeline if relevant (e.g. code review output as issue body)
- If the intent is ambiguous, pick the most likely tool`, intent))

	return sb.String()
}

// parseDecision extracts the tool name and JSON args from the AI's response.
// Expected format:
//
//	TOOL: create_issue
//	ARGS: {"owner": "x", "repo": "y", "title": "z"}
func parseDecision(response string) (toolName, argsJSON string, err error) {
	lines := strings.Split(strings.TrimSpace(response), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TOOL:") {
			toolName = strings.TrimSpace(strings.TrimPrefix(line, "TOOL:"))
		}
		if strings.HasPrefix(line, "ARGS:") {
			argsJSON = strings.TrimSpace(strings.TrimPrefix(line, "ARGS:"))
		}
	}

	if toolName == "" {
		return "", "", fmt.Errorf("no TOOL: line found in response")
	}
	if argsJSON == "" {
		argsJSON = "{}"
	}

	// Validate JSON
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &check); err != nil {
		// Try to extract JSON if it's wrapped in a code block
		if idx := strings.Index(argsJSON, "{"); idx != -1 {
			argsJSON = argsJSON[idx:]
			if end := strings.LastIndex(argsJSON, "}"); end != -1 {
				argsJSON = argsJSON[:end+1]
			}
		}
		// Validate again
		if err := json.Unmarshal([]byte(argsJSON), &check); err != nil {
			return "", "", fmt.Errorf("ARGS is not valid JSON: %w", err)
		}
	}

	return toolName, argsJSON, nil
}
