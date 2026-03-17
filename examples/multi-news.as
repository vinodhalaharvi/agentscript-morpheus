(
  news "golang 2026"
  <*> news "rust lang 2026"
  <*> news "AI agents 2026"
  <*> rss "golang"
  <*> rss "google-ai"
)
>=> merge
>=> summarize
>=> ask "Give me a concise digest of the top stories across all these sources. Group by topic."
>=> (
  email "vinod.halaharvi@gmail.com"
  <*> notify "slack"
)
