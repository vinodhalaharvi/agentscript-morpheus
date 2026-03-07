// Generate 5 AI art images of DC landmarks with Gemini Imagen, send gallery to Slack
( image_generate "Washington Monument at sunset, cyberpunk neon style, dramatic lighting"
  <*> image_generate "Lincoln Memorial reflected in pool, watercolor painting style, dreamy"
  <*> image_generate "US Capitol building, Van Gogh starry night style, swirling sky"
  <*> image_generate "Cherry blossoms Tidal Basin, Japanese ukiyo-e woodblock print style"
  <*> image_generate "Georgetown waterfront at night, synthwave retrowave aesthetic, purple pink"
)
>=> merge
>=> ask "Create a fun art gallery description for these 5 AI-generated DC landmark images. Rate each one, pick a winner, and write it like an art critic who's had too much coffee."
>=> notify "slack"
