package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps MCPClient as a Plugin.
// Note: MCPClient is stateful — connected servers persist across commands
// within a session. The plugin holds a single shared client instance.
type Plugin struct {
	client *MCPClient
}

// NewPlugin creates an MCP plugin with a shared client.
func NewPlugin(client *MCPClient) *Plugin {
	return &Plugin{client: client}
}

func (p *Plugin) Name() string { return "mcp" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"mcp_connect": p.connect,
		"mcp_list":    p.list,
		"mcp":         p.call,
	}
}

func (p *Plugin) connect(ctx context.Context, args []string, input string) (string, error) {
	serverName, err := plugin.RequireArg(args, 0, "server name")
	if err != nil {
		return "", fmt.Errorf("mcp_connect: %w — usage: mcp_connect \"name\" \"command\"", err)
	}
	cmd, err := plugin.RequireArg(args, 1, "command")
	if err != nil {
		return "", fmt.Errorf("mcp_connect: %w — usage: mcp_connect \"name\" \"npx -y @modelcontextprotocol/server-xxx\"", err)
	}

	// Prompt for OAuth tokens if needed
	if strings.Contains(cmd, "server-github") &&
		os.Getenv("GITHUB_TOKEN") == "" &&
		os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN") == "" {
		fmt.Printf("⚠️  GitHub MCP server requires a token.\n")
		fmt.Printf("   Set GITHUB_TOKEN or GITHUB_PERSONAL_ACCESS_TOKEN\n")
	}

	if err := p.client.Connect(ctx, serverName, cmd); err != nil {
		return "", fmt.Errorf("failed to connect to MCP server %q: %w", serverName, err)
	}

	tools, _ := p.client.ListTools(serverName)
	return fmt.Sprintf("✅ Connected to MCP server %q (%d tools available)", serverName, len(tools)), nil
}

func (p *Plugin) list(ctx context.Context, args []string, input string) (string, error) {
	serverName := plugin.Arg(args, 0)

	if serverName == "" {
		servers := p.client.ListServers()
		if len(servers) == 0 {
			return "No MCP servers connected. Use mcp_connect first.", nil
		}
		var sb strings.Builder
		sb.WriteString("Connected MCP servers:\n")
		for _, s := range servers {
			tools, _ := p.client.ListTools(s)
			sb.WriteString(fmt.Sprintf("  - %s (%d tools)\n", s, len(tools)))
		}
		return sb.String(), nil
	}

	tools, err := p.client.ListTools(serverName)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tools for '%s':\n", serverName))
	for _, tool := range tools {
		sb.WriteString(fmt.Sprintf("\n%s\n  Description: %s\n", tool.Name, tool.Description))
		if tool.InputSchema != nil {
			if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
				sb.WriteString("  Parameters:\n")
				for name, schema := range props {
					if s, ok := schema.(map[string]interface{}); ok {
						sb.WriteString(fmt.Sprintf("    - %s: %v\n", name, s["description"]))
					}
				}
			}
		}
	}
	return sb.String(), nil
}

func (p *Plugin) call(ctx context.Context, args []string, input string) (string, error) {
	arg, err := plugin.RequireArg(args, 0, "server:tool")
	if err != nil {
		return "", fmt.Errorf("mcp: %w — usage: mcp \"server:tool\" '{\"arg\": \"value\"}'", err)
	}
	parts := strings.SplitN(arg, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid mcp call format — use: mcp \"server:tool\" '{\"arg\": \"value\"}'")
	}
	serverName := strings.TrimSpace(parts[0])
	toolName := strings.TrimSpace(parts[1])
	argsJSON := plugin.Arg(args, 1)

	fmt.Printf("🔧 Calling %s.%s...\n", serverName, toolName)
	result, err := p.client.CallTool(ctx, serverName, toolName, argsJSON)
	if err != nil {
		return "", fmt.Errorf("MCP call failed: %w", err)
	}
	fmt.Printf("✅ MCP call complete\n")
	return result, nil
}
