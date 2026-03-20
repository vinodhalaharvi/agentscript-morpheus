mcp_connect "airbnb" "npx -y @openbnb/mcp-server-airbnb --ignore-robots-txt"
mcp_connect "maps" "npx -y @modelcontextprotocol/server-google-maps"
>=> mcp_agent "airbnb" "search airbnb stays in New York April 10-15 2026 for 2 guests under $300 per night"
>=> maps_trip "NYC Airbnb Options"
