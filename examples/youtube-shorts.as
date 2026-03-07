( ( ask "write a 30 second narration about why butterflies are amazing, keep it engaging for social media"
    >=> text_to_speech "Kore"
    >=> save "narration.wav"
  )
  <*> ( ( image_generate "monarch butterfly close up on purple flower, macro photography" >=> save "butterfly1.png"
          <*> image_generate "butterflies flying through sunlit garden, dreamy atmosphere" >=> save "butterfly2.png"
        )
        >=> merge
        >=> images_to_video "butterfly1.png butterfly2.png"
        >=> save "butterfly_video.mp4"
      )
)
>=> merge
>=> audio_video_merge "butterfly_shorts.mp4"
>=> youtube_shorts "Amazing Butterflies - AI Generated"
