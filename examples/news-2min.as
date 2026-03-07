( search "top US news headlines today"
  >=> ask "Extract the top 10 news headlines from these results. For each headline, give just the clean headline text without any special characters, emojis, hashtags, or URLs. Format as a numbered list."
  >=> ask "Write a 2-minute news anchor script covering these 10 headlines in detail. For each headline, provide 2-3 sentences of context. Make it sound professional but engaging, like a morning news briefing. Start with 'Good morning, here are today's top US news headlines' and end with 'That's your news update for today. Stay informed, stay engaged.'"
  >=> text_to_speech "Charon"
  >=> save "news_narration.wav"
  <*> image_generate "professional TV news studio, anchor desk with multiple screens showing world map and news graphics, blue and red lighting, 4k cinematic, photorealistic" >=> save "news_bg.png"
)
>=> merge
>=> image_audio_merge "news_2min.mp4"
>=> youtube_upload "Top 10 US News Headlines Today - Full Briefing"
