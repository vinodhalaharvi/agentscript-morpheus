( search "Gemini API features pricing" >=> summarize
  <*> search "OpenAI GPT-4 API features pricing" >=> summarize
  <*> search "Anthropic Claude API features pricing" >=> summarize
)
>=> merge
>=> ask "Create a comparison table. Which API should a startup choose?"
>=> save "ai-comparison.md"
