( search "tech industry trends Q4 2024" >=> analyze "key trends"
  <*> search "AI market growth projections 2025" >=> analyze "growth potential"
  <*> search "enterprise software spending 2024" >=> analyze "budget trends"
)
>=> merge
>=> ask "Write a 500-word executive summary for a board presentation"
>=> save "board-summary.md"
>=> email "board@company.com"
