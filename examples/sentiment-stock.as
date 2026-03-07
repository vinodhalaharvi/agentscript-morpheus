// Sentiment-Aware Stock Monitor
( stock "NVDA"
  <*> news "NVIDIA earnings AI" >=> hf_classify "ProsusAI/finbert"
)
>=> merge
>=> ask "Analyze NVIDIA: combine price data with news sentiment"
>=> save "nvda-analysis.md"
