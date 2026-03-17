(
  news "golang 2026"
  <*> news "rust lang 2026"
  <*> news "AI agents 2026"
)
>=> merge
>=> summarize
>=> ask "Give me a concise digest of the top stories across these topics"
>=> (
  email "vinod.halaharvi@gmail.com"
  <*> notify "slack"
)
