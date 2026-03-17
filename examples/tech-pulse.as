(
  perplexity_recent "Go language news" "week"
  <*> perplexity_recent "Rust language news" "week"
  <*> perplexity_recent "AI agents and LLMs" "week"
  <*> stock "NVDA,MSFT,GOOG"
)
>=> merge
>=> ask "Give me a CTO morning briefing. Key developments, what matters, what to watch."
>=> (
  email "vinod.halaharvi@gmail.com"
  <*> notify "slack"
)
