// Connect to filesystem MCP
mcp_connect "fs" "npx -y @modelcontextprotocol/server-filesystem ."

// Read the grammar file
>=> mcp "fs:read_file" '{"path": "grammar.go"}'

// Generate documentation
>=> ask "Document this Go grammar file. Explain the Morpheus DSL syntax, available commands, and how parsing works."

// Save as a Google Doc
>=> doc_create "AgentScript Grammar Documentation"

// Email it
>=> email "vinod.halaharvi@gmail.com"
