package main

import (
	"context"
	"fmt"
	"strings"
)

// Translator converts natural language to AgentScript DSL
type Translator struct {
	gemini *GeminiClient
}

// NewTranslator creates a new Translator
func NewTranslator(ctx context.Context, apiKey string) (*Translator, error) {
	client := NewGeminiClient(apiKey, "")

	return &Translator{
		gemini: client,
	}, nil
}

const systemPrompt = `You are a translator that converts natural language into AgentScript DSL.

AgentScript is a simple command language with these commands:

CORE: search, summarize, save, read, ask, analyze, list, merge
GOOGLE: email, calendar, meet, drive_save, doc_create, sheet_create, sheet_append, task, youtube_search
MULTIMODAL: image_generate, image_analyze, video_generate, video_analyze, images_to_video, text_to_speech, translate
DATA (most free, no key): weather "location", crypto "BTC,ETH", reddit "r/subreddit", rss "hn", news "query", news_headlines "category", stock "AAPL", job_search "query" "location", twitter "query"
NOTIFICATIONS: notify "slack", whatsapp "+number"
HUGGING FACE: hf_classify, hf_ner, hf_summarize, hf_translate "text" "en" "fr", hf_generate, hf_zero_shot "text" "labels", hf_qa, hf_embeddings, hf_image_generate, hf_similarity
CONTROL: parallel { }, if "condition", foreach "separator"

Commands chain with -> (pipe): search "topic" -> summarize -> save "notes.md"
Parallel blocks: parallel { search "A" -> analyze  search "B" -> analyze } -> merge -> ask "compare"

Rules:
1. Output ONLY the DSL commands, no explanation
2. Use double quotes for all string arguments
3. Chain commands logically with ->
4. Keep it simple - minimum commands needed
5. Use parallel when comparing multiple items
6. Always use merge after parallel

Examples:
- "what is the weather in NYC" -> weather "New York"
- "check bitcoin price" -> crypto "BTC"
- "whats on hacker news" -> rss "hn"
- "find golang jobs" -> job_search "golang contract" "remote"
- "morning briefing" -> parallel { weather "SF" crypto "BTC,ETH" news_headlines "technology" rss "hn" } -> merge -> ask "morning briefing"
- "analyze nvidia sentiment" -> news "NVIDIA" -> hf_classify
- "compare AWS and GCP" -> parallel { search "AWS strengths" -> analyze search "GCP strengths" -> analyze } -> merge -> ask "compare"
`

// Translate converts natural language to AgentScript DSL
func (t *Translator) Translate(ctx context.Context, naturalLanguage string) (string, error) {
	prompt := fmt.Sprintf("%s\n\nConvert this to AgentScript:\n%s", systemPrompt, naturalLanguage)

	result, err := t.gemini.GenerateContent(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("translation failed: %w", err)
	}

	// Clean up the response
	dsl := strings.TrimSpace(result)

	// Remove markdown code blocks if present
	dsl = strings.TrimPrefix(dsl, "```agentscript")
	dsl = strings.TrimPrefix(dsl, "```")
	dsl = strings.TrimSuffix(dsl, "```")
	dsl = strings.TrimSpace(dsl)

	return dsl, nil
}
