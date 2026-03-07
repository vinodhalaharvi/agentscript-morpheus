( weather "Ashburn"
  <*> crypto "BTC,ETH,SOL"
  <*> rss "hn"
  <*> reddit "r/golang"
)
>=> merge
>=> ask "Create a concise morning briefing with sections for Weather, Crypto, Tech News, and Golang Community"
>=> notify "slack"
