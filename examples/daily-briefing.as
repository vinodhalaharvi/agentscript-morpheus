// Daily Morning Briefing — weather, crypto, stocks, news, HN, Reddit, jobs
( weather "San Francisco"
  <*> crypto "BTC,ETH,SOL"
  <*> stock "AAPL,GOOGL,MSFT,NVDA,META"
  <*> news_headlines "technology"
  <*> rss "hn"
  <*> reddit "r/golang" "top"
  <*> job_search "golang contract" "remote"
)
>=> merge
>=> ask "Professional morning briefing: 1) Weather 2) Crypto 3) Stocks 4) Headlines 5) HN 6) Reddit 7) Jobs"
>=> save "briefing.md"
