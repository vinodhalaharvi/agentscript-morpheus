// ============================================
// Local News YouTube Shorts Pipeline
// ============================================
// Uses Veo 3.1's NATIVE synchronized audio
// NO separate TTS needed - Veo generates speech!
// ============================================

search "local news San Francisco today"
>=> summarize "Extract top 2 headlines in 2 short sentences"
>=> video_script "news anchor"
>=> video_generate "vertical shorts"
>=> save "sf_news.mp4"
>=> confirm "Upload to YouTube Shorts?"
>=> youtube_shorts "SF Local News Update"
