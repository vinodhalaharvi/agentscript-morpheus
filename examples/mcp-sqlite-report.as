// Connect to SQLite MCP server (Python version)
mcp_connect "sqlite" "uvx mcp-server-sqlite --db-path ./test.db"

// Query the database
>=> mcp "sqlite:read_query" '{"query": "SELECT * FROM users"}'

// Generate analytics report
>=> ask "Create a user analytics report with insights, trends, and recommendations"

// Save as Google Doc
>=> doc_create "User Analytics Report"

// Email the report
>=> email "vinod.halaharvi@gmail.com"
