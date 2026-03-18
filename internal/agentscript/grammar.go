package agentscript

import (
	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// Program represents a complete AgentScript program
type Program struct {
	Statements []*Statement `@@*`
}

// Statement can be a command or a fan-out group ( a <*> b <*> c )
type Statement struct {
	Parallel *Parallel  `( "(" @@ ")" |`
	Command  *Command   `  @@ )`
	Pipe     *Statement `( ">=>" @@ )?`
}

// Parallel represents a fan-out group: ( branch <*> branch <*> ... )
// Uses the Morpheus fan-out operator <*> to separate concurrent branches.
// Branches accumulates all arms into the slice used by runtime.executeParallel.
type Parallel struct {
	Branches []*Statement `@@ ( "<*>" @@ )*`
}

// Command represents a single command with up to 3 string arguments
type Command struct {
	Action string `@(
		"search" | "summarize" | "save" | "read" | "stdin" | "ask" |
		"analyze" | "list" | "merge" | "email" | "calendar" | "meet" |
		"drive_save" | "doc_create" | "sheet_append" | "sheet_create" |
		"task" | "contact_find" | "youtube_search" | "youtube_upload" |
		"youtube_shorts" | "image_generate" | "image_analyze" |
		"video_analyze" | "video_generate" | "images_to_video" |
		"text_to_speech" | "audio_video_merge" | "image_audio_merge" |
		"maps_trip" | "form_create" | "form_responses" | "translate" |
		"places_search" | "mcp_connect" | "mcp_list" | "mcp" |
		"video_script" | "confirm" | "github_pages_html" | "github_pages" |
		"job_search" | "weather" | "news_headlines" | "news" | "stock" |
		"crypto" | "reddit" | "rss" | "notify" | "whatsapp" | "twitter" |
		"foreach" | "if" | "hf_generate" | "hf_summarize" | "hf_classify" |
		"hf_ner" | "hf_translate" | "hf_embeddings" | "hf_qa" |
		"hf_fill_mask" | "hf_zero_shot" | "hf_image_generate" |
		"hf_image_classify" | "hf_speech_to_text" | "hf_similarity" |
		"emoji_style" | "perplexity" | "perplexity_pro" | "perplexity_recent" | "perplexity_domain" | "agent"
	)`
	Arg  string `@String?`
	Arg2 string `@String?`
	Arg3 string `@String?`
}

// Lexer definition — Morpheus operators replace the old -> and parallel{} syntax
var scriptLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Comment", Pattern: `//[^\n]*\n?`},
	{Name: "Keyword", Pattern: `(search|summarize|save|read|stdin|ask|analyze|list|merge|email|calendar|meet|drive_save|doc_create|sheet_append|sheet_create|task|contact_find|youtube_search|youtube_upload|youtube_shorts|image_generate|image_analyze|video_analyze|video_generate|images_to_video|text_to_speech|audio_video_merge|image_audio_merge|maps_trip|form_create|form_responses|translate|places_search|mcp_connect|mcp_list|mcp|video_script|confirm|github_pages|github_pages_html|job_search|weather|news_headlines|news|stock|crypto|reddit|rss|notify|whatsapp|twitter|foreach|if|hf_generate|hf_summarize|hf_classify|hf_ner|hf_translate|hf_embeddings|hf_qa|hf_fill_mask|hf_zero_shot|hf_image_generate|hf_image_classify|hf_speech_to_text|hf_similarity|emoji_style|perplexity_domain|perplexity_recent|perplexity_pro|perplexity|agent)`},
	{Name: "String", Pattern: `"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`},
	{Name: "Kleisli", Pattern: `>=>`},
	{Name: "FanOut", Pattern: `<\*>`},
	{Name: "LParen", Pattern: `\(`},
	{Name: "RParen", Pattern: `\)`},
	{Name: "Whitespace", Pattern: `[ \t\n\r]+`},
})

// Parser instance
var Parser = participle.MustBuild[Program](
	participle.Lexer(scriptLexer),
	participle.Elide("Whitespace", "Comment"),
	participle.Unquote("String"),
)

// Parse parses a Morpheus AgentScript program from a string
func Parse(input string) (*Program, error) {
	return Parser.ParseString("", input)
}
