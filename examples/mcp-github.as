// Connect to GitHub MCP server
mcp_connect "github" "npx -y @modelcontextprotocol/server-github"

// Search for repositories
>=> mcp "github:search_repositories" '{"query": "agentscript"}'

// Summarize results
>=> summarize
