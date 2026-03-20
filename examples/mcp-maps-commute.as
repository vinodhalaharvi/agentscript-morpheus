mcp_connect "maps" "npx -y @modelcontextprotocol/server-google-maps"
>=> mcp_agent "maps" "get driving directions from Times Square New York to JFK Airport"
>=> ask "Summarize the route, highlight any major highways, and estimate the best time to travel to avoid traffic"
>=> save "nyc-to-jfk.md"
