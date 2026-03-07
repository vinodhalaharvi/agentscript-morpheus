// Tech Digest from multiple feeds
( rss "hn"
  <*> rss "lobsters"
  <*> reddit "r/golang" "top"
  <*> news_headlines "technology"
)
>=> merge
>=> summarize
>=> save "tech-digest.md"
