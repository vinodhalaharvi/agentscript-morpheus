mcp_connect "yt" "npx -y yt-mcp"
>=> (
  mcp_agent "yt" "get transcript of the most viewed Go conference talk this year"
  <*> perplexity_recent "Go programming trends 2026" "month"
)
>=> merge
>=> ask "What should I learn about Go based on conference talks and current trends?"
>=> email "vinod.halaharvi@gmail.com"
