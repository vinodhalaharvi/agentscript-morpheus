// Connect to Fetch MCP server
mcp_connect "fetch" "npx -y @modelcontextprotocol/server-fetch"

// Fetch Hacker News
>=> mcp "fetch:fetch" '{"url": "https://news.ycombinator.com"}'

// Summarize top stories
>=> ask "Extract and summarize the top 5 stories from this page"

// Email the summary
>=> email "vinod.halaharvi@gmail.com"
