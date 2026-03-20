mcp_connect "airbnb" "npx -y @openbnb/mcp-server-airbnb --ignore-robots-txt"
>=> mcp_agent "airbnb" "search for airbnb stays in New York for 2 adults checking in April 10 2026 checking out April 15 2026"
>=> ask "Summarize the top 5 options with name, price per night, location and a direct booking link"
>=> save "nyc-stays.md"
