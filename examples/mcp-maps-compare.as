mcp_connect "maps" "npx -y @modelcontextprotocol/server-google-maps"
>=> (
  mcp_agent "maps" "driving distance and time from San Francisco to Los Angeles"
  <*> mcp_agent "maps" "driving distance and time from San Francisco to Las Vegas"
  <*> mcp_agent "maps" "driving distance and time from San Francisco to Portland"
)
>=> merge
>=> ask "Compare these three road trips from San Francisco. Which is best for a weekend getaway and why?"
>=> save "sf-road-trips.md"
