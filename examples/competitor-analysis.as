( search "Google company strengths weaknesses" >=> analyze "competitive position"
  <*> search "Microsoft company strengths weaknesses" >=> analyze "competitive position"
)
>=> merge
>=> ask "Based on this analysis, which company is winning and why?"
>=> save "competitor-analysis.md"
