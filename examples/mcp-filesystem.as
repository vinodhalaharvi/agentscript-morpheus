// Connect to filesystem MCP server
mcp_connect "fs" "npx -y @modelcontextprotocol/server-filesystem ."

// List current directory
>=> mcp "fs:list_directory" '{"path": "."}'

// Summarize the listing
>=> summarize
