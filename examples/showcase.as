( image_generate "serene mountain lake at sunrise with morning mist, monarch butterflies taking flight" >=> save "dawn.png"
  <*> image_generate "same mountain lake at golden sunset, butterflies silhouetted against orange sky" >=> save "sunset.png"
)
>=> merge
>=> images_to_video "dawn.png sunset.png"
>=> save "butterflies.mp4"
>=> drive_save "butterflies"
>=> email "vinodhalaharvi@gmail.com"
