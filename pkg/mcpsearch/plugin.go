// Package mcpsearch searches the MCP registry for servers.
//
// Commands:
//
//	mcp_search "query"           — search official registry + glama
//	mcp_search_install "name"    — output the mcp_connect command for a server
//
// API:
//
//	GET https://registry.modelcontextprotocol.io/v0/servers?search=query&limit=10
//
// Response shape (from real API docs):
//
//	{
//	  "servers": [{
//	    "server": {
//	      "name": "io.github.username/server-name",
//	      "description": "...",
//	      "repository": { "url": "https://github.com/..." },
//	      "version": "1.0.0",
//	      "packages": [{
//	        "registryType": "npm",
//	        "identifier": "@scope/package-name",
//	        "version": "1.0.0",
//	        "transport": { "type": "stdio" },
//	        "environmentVariables": [{ "name": "API_KEY", "isRequired": true }]
//	      }]
//	    }
//	  }],
//	  "metadata": { "nextCursor": "..." }
//	}
package mcpsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

const (
	registryURL = "https://registry.modelcontextprotocol.io/v0/servers"
	glamaURL    = "https://glama.ai/api/mcp/v1/servers"
)

// Plugin provides mcp_search and mcp_search_install commands.
type Plugin struct {
	client  *http.Client
	verbose bool
}

// NewPlugin creates an mcpsearch plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{
		client:  &http.Client{Timeout: 15 * time.Second},
		verbose: verbose,
	}
}

func (p *Plugin) Name() string { return "mcpsearch" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		// mcp_search "home automation"
		"mcp_search": p.search,
		// mcp_search_install "io.github.user/server-name"
		"mcp_search_install": p.searchInstall,
	}
}

// --- Registry API types ---

type registryResponse struct {
	Servers  []registryEntry `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor"`
	} `json:"metadata"`
}

type registryEntry struct {
	Server registryServer `json:"server"`
}

type registryServer struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Repository  *registryRepo     `json:"repository"`
	Packages    []registryPackage `json:"packages"`
}

type registryRepo struct {
	URL string `json:"url"`
}

type registryPackage struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier"`
	Version      string `json:"version"`
	RuntimeHint  string `json:"runtimeHint"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
	EnvironmentVariables []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsRequired  bool   `json:"isRequired"`
		IsSecret    bool   `json:"isSecret"`
	} `json:"environmentVariables"`
}

// search searches the official MCP registry.
// Usage: mcp_search "home automation"
//
//	mcp_search "sqlite database"
//	mcp_search "grpc"
func (p *Plugin) search(ctx context.Context, args []string, input string) (string, error) {
	query := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if query == "" {
		return "", fmt.Errorf("mcp_search requires a query — e.g. mcp_search \"home automation\"")
	}

	fmt.Printf("🔍 Searching MCP registry for: %q\n", query)

	servers, err := p.searchRegistry(ctx, query, 10)
	if err != nil {
		return "", fmt.Errorf("mcp_search: registry error: %w", err)
	}

	if len(servers) == 0 {
		return fmt.Sprintf("No MCP servers found for %q in the official registry.\n\nTry broader terms or check:\n- https://glama.ai/mcp/servers\n- https://pulsemcp.com\n- https://mcp.so", query), nil
	}

	return formatResults(query, servers), nil
}

// searchInstall outputs the mcp_connect command for a named server.
// Usage: mcp_search_install "io.github.user/server-name"
func (p *Plugin) searchInstall(ctx context.Context, args []string, input string) (string, error) {
	name := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if name == "" {
		return "", fmt.Errorf("mcp_search_install requires a server name")
	}

	fmt.Printf("📦 Looking up install command for: %q\n", name)

	// Search for the specific server
	servers, err := p.searchRegistry(ctx, name, 5)
	if err != nil {
		return "", fmt.Errorf("mcp_search_install: %w", err)
	}

	// Find exact or closest match
	for _, s := range servers {
		if strings.EqualFold(s.Name, name) || strings.Contains(s.Name, name) {
			cmd := buildConnectCommand(s)
			if cmd != "" {
				return fmt.Sprintf("# %s\n# %s\n%s", s.Name, s.Description, cmd), nil
			}
		}
	}

	return fmt.Sprintf("Could not find install command for %q — check https://glama.ai/mcp/servers", name), nil
}

// searchRegistry calls the official MCP registry REST API.
func (p *Plugin) searchRegistry(ctx context.Context, query string, limit int) ([]registryServer, error) {
	apiURL := fmt.Sprintf("%s?search=%s&limit=%d",
		registryURL,
		url.QueryEscape(query),
		limit,
	)

	if p.verbose {
		fmt.Printf("[mcpsearch] GET %s\n", apiURL)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AgentScript/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result registryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse registry response: %w", err)
	}

	servers := make([]registryServer, len(result.Servers))
	for i, e := range result.Servers {
		servers[i] = e.Server
	}
	return servers, nil
}

// formatResults formats search results as readable text with install commands.
func formatResults(query string, servers []registryServer) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Found %d MCP servers for %q:\n\n", len(servers), query))

	for i, s := range servers {
		sb.WriteString(fmt.Sprintf("## %d. %s\n", i+1, s.Name))
		if s.Description != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", s.Description))
		}
		if s.Repository != nil && s.Repository.URL != "" {
			sb.WriteString(fmt.Sprintf("   GitHub: %s\n", s.Repository.URL))
		}

		// Build the mcp_connect command
		cmd := buildConnectCommand(s)
		if cmd != "" {
			sb.WriteString(fmt.Sprintf("   Install:\n   %s\n", cmd))
		}

		// Show required env vars
		for _, pkg := range s.Packages {
			var required []string
			for _, env := range pkg.EnvironmentVariables {
				if env.IsRequired {
					required = append(required, env.Name)
				}
			}
			if len(required) > 0 {
				sb.WriteString(fmt.Sprintf("   Requires: %s\n", strings.Join(required, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("─────────────────────────────────────────\n")
	sb.WriteString("More servers: https://glama.ai/mcp/servers\n")

	return sb.String()
}

// buildConnectCommand generates the mcp_connect DSL command for a server.
func buildConnectCommand(s registryServer) string {
	// Extract short name for the alias
	parts := strings.Split(s.Name, "/")
	alias := parts[len(parts)-1]
	// Clean up the alias
	alias = strings.ReplaceAll(alias, "server-", "")
	alias = strings.ReplaceAll(alias, "-mcp", "")
	alias = strings.ReplaceAll(alias, "mcp-", "")
	if alias == "" {
		alias = "server"
	}

	for _, pkg := range s.Packages {
		switch strings.ToLower(pkg.RegistryType) {
		case "npm":
			hint := "npx"
			if pkg.RuntimeHint != "" {
				hint = pkg.RuntimeHint
			}
			return fmt.Sprintf("mcp_connect \"%s\" \"%s -y %s\"",
				alias, hint, pkg.Identifier)
		case "pypi":
			return fmt.Sprintf("mcp_connect \"%s\" \"uvx %s\"",
				alias, pkg.Identifier)
		case "docker", "oci":
			return fmt.Sprintf("mcp_connect \"%s\" \"docker run -i %s\"",
				alias, pkg.Identifier)
		}
	}
	return ""
}
