mcp_connect "ha" "npx -y @jango-blockchained/homeassistant-mcp@latest"
>=> mcp_agent "ha" "get state of event.front_door_motion, event.front_door_ding, sensor.front_door_last_activity and sensor.front_door_battery"
>=> ask "Summarize the current state of my Ring front door. When was last activity? Is battery ok?"
>=> save "ring-status.md"
