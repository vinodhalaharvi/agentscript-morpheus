( ( ( search "AI trends 2026" >=> summarize
      <*> search "machine learning breakthroughs" >=> summarize
    ) >=> merge >=> ask "combine these into key insights"
  )
  <*> ( ( image_generate "futuristic AI robot in neon city" >=> save "robot.png"
          <*> image_generate "same robot at sunset, cinematic" >=> save "robot_sunset.png"
        )
        >=> merge
        >=> images_to_video "robot.png robot_sunset.png"
        >=> save "robot_video.mp4"
      )
)
>=> merge
>=> ask "create an executive summary of AI trends with visual content description"
>=> save "ai_report.md"
>=> doc_create "AI Trends Report 2026"
>=> email "vinodhalaharvi@gmail.com"
