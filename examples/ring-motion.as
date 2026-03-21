mcp_connect "ha" "npx -y @jango-blockchained/homeassistant-mcp@latest"
>=> mcp_agent "ha" "use get_history tool to get history for sensor.front_door_last_activity"
>=> ask "Someone was detected at my front door. Write a 10 word security alert based on this activity."
>=> save "ring-alert.md"
