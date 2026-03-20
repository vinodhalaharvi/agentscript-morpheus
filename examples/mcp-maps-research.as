mcp_connect "maps" "npx -y @modelcontextprotocol/server-google-maps"
>=> mcp_agent "maps" "find the top 5 rated restaurants near Times Square New York"
>=> ask "Summarize these restaurants with cuisine type, rating, and why someone should visit each"
>=> save "nyc-restaurants.md"
