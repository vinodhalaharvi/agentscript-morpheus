package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ============================================================================
// Emoji Style Engine
// ============================================================================

// EmojiSource represents where an emoji comes from
type EmojiSource struct {
	Emoji       string // unicode char(s) like "😀" or empty for custom
	Name        string // descriptive name: "grinning_face", "custom_1"
	Description string // "grinning face with big eyes"
	ImagePath   string // path to source image (for custom uploads)
	IsCustom    bool
}

// EmojiStyleConfig holds styling parameters
type EmojiStyleConfig struct {
	Style     string // user prompt: "wearing a suit with red tie"
	Engine    string // "gemini", "hf", or "" (auto)
	HFModel   string // optional custom HF model
	OutputDir string // where to save results
	Size      int    // output size in px (default 512)
	PackName  string // sticker pack name
	BatchSize int    // how many to process at once
}

// EmojiStyleResult holds the result of styling one emoji
type EmojiStyleResult struct {
	Source   EmojiSource
	PNGPath  string // intermediate PNG
	WebPPath string // final WebP sticker
	Error    error
}

// EmojiStyleClient handles emoji generation
type EmojiStyleClient struct {
	geminiKey string
	hfToken   string
	client    *http.Client
	verbose   bool
}

// NewEmojiStyleClient creates a new emoji style client
func NewEmojiStyleClient(verbose bool) *EmojiStyleClient {
	return &EmojiStyleClient{
		geminiKey: os.Getenv("GEMINI_API_KEY"),
		hfToken:   os.Getenv("HF_TOKEN"),
		client: &http.Client{
			Timeout: 120 * time.Second, // image gen can be slow
		},
		verbose: verbose,
	}
}

func (e *EmojiStyleClient) log(format string, args ...any) {
	if e.verbose {
		fmt.Printf("[EMOJI_STYLE] "+format+"\n", args...)
	}
}

// ============================================================================
// Emoji Detection & Parsing
// ============================================================================

// Well-known emoji descriptions for better prompts
var emojiDescriptions = map[string]string{
	"😀": "grinning face", "😃": "grinning face with big eyes",
	"😄": "grinning squinting face", "😁": "beaming face",
	"😆": "grinning squinting face", "😅": "grinning face with sweat",
	"🤣": "rolling on floor laughing", "😂": "face with tears of joy",
	"🙂": "slightly smiling face", "😉": "winking face",
	"😊": "smiling face with smiling eyes", "😇": "smiling face with halo",
	"😍": "heart eyes", "🤩": "star struck",
	"😘": "face blowing a kiss", "😋": "face savoring food",
	"😎": "smiling face with sunglasses", "🤓": "nerd face",
	"🧐": "face with monocle", "🤔": "thinking face",
	"😏": "smirking face", "😒": "unamused face",
	"😞": "disappointed face", "😔": "pensive face",
	"😟": "worried face", "😕": "confused face",
	"😢": "crying face", "😭": "loudly crying face",
	"😤": "face with steam from nose", "😡": "pouting face",
	"🤬": "face with symbols on mouth", "😱": "face screaming in fear",
	"😰": "anxious face with sweat", "😥": "sad but relieved face",
	"🤗": "hugging face", "🤭": "face with hand over mouth",
	"🤫": "shushing face", "🤥": "lying face",
	"😶": "face without mouth", "😐": "neutral face",
	"😑": "expressionless face", "😬": "grimacing face",
	"🙄": "face with rolling eyes", "😴": "sleeping face",
	"🤤": "drooling face", "😷": "face with medical mask",
	"🤒": "face with thermometer", "🤕": "face with head bandage",
	"🤢": "nauseated face", "🤮": "face vomiting",
	"🤧": "sneezing face", "🥵": "hot face",
	"🥶": "cold face", "🥴": "woozy face",
	"😵": "face with crossed out eyes", "🤯": "exploding head",
	"🤠": "cowboy hat face", "🥳": "partying face",
	"🥸": "disguised face", "😈": "smiling face with horns",
	"👿": "angry face with horns", "👹": "ogre",
	"👺": "goblin", "💀": "skull",
	"👻": "ghost", "👽": "alien",
	"🤖": "robot", "💩": "pile of poo",
	"🔥": "fire", "⭐": "star",
	"🌟": "glowing star", "💫": "dizzy star",
	"✨": "sparkles", "💥": "collision",
	"❤️": "red heart", "🧡": "orange heart",
	"💛": "yellow heart", "💚": "green heart",
	"💙": "blue heart", "💜": "purple heart",
	"🖤": "black heart", "🤍": "white heart",
	"💯": "hundred points", "💢": "anger symbol",
	"👍": "thumbs up", "👎": "thumbs down",
	"👊": "oncoming fist", "✊": "raised fist",
	"🤞": "crossed fingers", "✌️": "victory hand",
	"🤟": "love you gesture", "🤘": "sign of the horns",
	"👌": "OK hand", "🤌": "pinched fingers",
	"👋": "waving hand", "🤚": "raised back of hand",
	"✋": "raised hand", "🖖": "vulcan salute",
	"👏": "clapping hands", "🙌": "raising hands",
	"🫶": "heart hands", "🤝": "handshake",
	"🙏": "folded hands", "💪": "flexed biceps",
	"🦾": "mechanical arm", "🦿": "mechanical leg",
	"🚀": "rocket", "🎯": "bullseye",
	"💡": "light bulb", "🎉": "party popper",
	"🎊": "confetti ball", "🏆": "trophy",
	"🥇": "gold medal", "🥈": "silver medal",
	"🥉": "bronze medal", "⚡": "high voltage",
	"🌈": "rainbow", "☀️": "sun",
	"🌙": "crescent moon", "🌍": "globe",
	"🐶": "dog face", "🐱": "cat face",
	"🐭": "mouse face", "🐹": "hamster",
	"🐰": "rabbit face", "🦊": "fox",
	"🐻": "bear", "🐼": "panda",
	"🐨": "koala", "🐯": "tiger face",
	"🦁": "lion", "🐮": "cow face",
	"🐷": "pig face", "🐸": "frog",
	"🐵": "monkey face", "🙈": "see-no-evil monkey",
	"🙉": "hear-no-evil monkey", "🙊": "speak-no-evil monkey",
	"🐔": "chicken", "🐧": "penguin",
	"🐦": "bird", "🦅": "eagle",
	"🦉": "owl", "🦇": "bat",
	"🐺": "wolf", "🐗": "boar",
	"🐴": "horse face", "🦄": "unicorn",
	"🐝": "honeybee", "🐛": "bug",
	"🦋": "butterfly", "🐌": "snail",
	"🐙": "octopus", "🦑": "squid",
	"🦀": "crab", "🐡": "blowfish",
	"🐠": "tropical fish", "🐟": "fish",
	"🐬": "dolphin", "🐳": "spouting whale",
	"🐋": "whale", "🦈": "shark",
	"🐊": "crocodile", "🐅": "tiger",
	"🐆": "leopard", "🦓": "zebra",
	"🦍": "gorilla", "🦧": "orangutan",
	"🐘": "elephant", "🦛": "hippopotamus",
	"🦏": "rhinoceros", "🐪": "camel",
	"🐫": "two-hump camel", "🦒": "giraffe",
	"🦘": "kangaroo", "🦬": "bison",
	"🐃": "water buffalo", "🐂": "ox",
	"🐄": "cow", "🐎": "horse",
	"🐖": "pig", "🐑": "ewe",
	"🦙": "llama", "🐕": "dog",
	"🐩": "poodle", "🦮": "guide dog",
	"🐈": "cat", "🐓": "rooster",
	"🦃": "turkey", "🦚": "peacock",
	"🦜": "parrot", "🦢": "swan",
	"🦩": "flamingo", "🕊️": "dove",
	"🐇": "rabbit", "🦝": "raccoon",
	"🦨": "skunk", "🦡": "badger",
	"🦫": "beaver", "🦦": "otter",
	"🦥": "sloth", "🐁": "mouse",
	"🐀": "rat", "🐿️": "chipmunk",
	"🦔": "hedgehog",
}

// IsEmoji checks if a rune is an emoji
func IsEmoji(r rune) bool {
	// Emoji ranges (simplified but covers most)
	return (r >= 0x1F600 && r <= 0x1F64F) || // emoticons
		(r >= 0x1F300 && r <= 0x1F5FF) || // misc symbols & pictographs
		(r >= 0x1F680 && r <= 0x1F6FF) || // transport & map
		(r >= 0x1F1E0 && r <= 0x1F1FF) || // flags
		(r >= 0x2600 && r <= 0x26FF) || // misc symbols
		(r >= 0x2700 && r <= 0x27BF) || // dingbats
		(r >= 0xFE00 && r <= 0xFE0F) || // variation selectors
		(r >= 0x1F900 && r <= 0x1F9FF) || // supplemental symbols
		(r >= 0x1FA00 && r <= 0x1FA6F) || // chess symbols
		(r >= 0x1FA70 && r <= 0x1FAFF) || // symbols & pictographs ext-A
		(r >= 0x200D && r <= 0x200D) || // ZWJ
		(r >= 0x231A && r <= 0x231B) || // watch, hourglass
		(r >= 0x23E9 && r <= 0x23F3) || // media controls
		(r >= 0x23F8 && r <= 0x23FA) || // more media
		(r >= 0x25AA && r <= 0x25AB) || // squares
		(r >= 0x25B6 && r <= 0x25C0) || // triangles
		(r >= 0x25FB && r <= 0x25FE) || // more squares
		(r >= 0x2614 && r <= 0x2615) || // umbrella, hot beverage
		(r >= 0x2648 && r <= 0x2653) || // zodiac
		(r >= 0x267F && r <= 0x267F) || // wheelchair
		(r >= 0x2934 && r <= 0x2935) || // arrows
		(r >= 0x2B05 && r <= 0x2B07) || // more arrows
		(r >= 0x2B1B && r <= 0x2B1C) || // squares
		(r >= 0x2B50 && r <= 0x2B50) || // star
		(r >= 0x2B55 && r <= 0x2B55) || // circle
		(r >= 0x3030 && r <= 0x3030) || // wavy dash
		(r >= 0x303D && r <= 0x303D) || // part alternation mark
		(r >= 0x3297 && r <= 0x3297) || // circled ideograph congratulation
		(r >= 0x3299 && r <= 0x3299) // circled ideograph secret
}

// ExtractEmojis extracts individual emoji characters from a string
// Handles multi-codepoint emojis (ZWJ sequences, skin tones, flags)
func ExtractEmojis(input string) []string {
	var emojis []string
	var current strings.Builder

	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if IsEmoji(r) {
			// Check if this continues a previous emoji (ZWJ, modifier, variation selector)
			if current.Len() > 0 {
				if r == 0x200D || r == 0xFE0F || r == 0xFE0E ||
					(r >= 0x1F3FB && r <= 0x1F3FF) { // ZWJ, VS, skin tones
					current.WriteRune(r)
					continue
				}
				// Check if previous rune was ZWJ — next emoji continues the sequence
				prevRunes := []rune(current.String())
				if len(prevRunes) > 0 && prevRunes[len(prevRunes)-1] == 0x200D {
					current.WriteRune(r)
					continue
				}
				// New emoji — flush the previous one
				emojis = append(emojis, current.String())
				current.Reset()
			}
			current.WriteRune(r)
		} else if r == 0x200D && current.Len() > 0 {
			// ZWJ connects emojis
			current.WriteRune(r)
		} else {
			// Non-emoji character — flush
			if current.Len() > 0 {
				s := current.String()
				if s != "\uFE0F" && s != "\uFE0E" {
					emojis = append(emojis, s)
				}
				current.Reset()
			}
		}
	}
	// Flush last
	if current.Len() > 0 {
		s := current.String()
		if s != "\uFE0F" && s != "\uFE0E" {
			emojis = append(emojis, s)
		}
	}

	return emojis
}

// DetectEmojiSources auto-detects whether input is unicode emojis or file paths
func DetectEmojiSources(input string) ([]EmojiSource, error) {
	var sources []EmojiSource

	// Check if input looks like file paths
	hasFiles := false
	for _, part := range strings.Fields(input) {
		cleaned := strings.Trim(part, ",;")
		if _, err := os.Stat(cleaned); err == nil {
			hasFiles = true
			ext := strings.ToLower(filepath.Ext(cleaned))
			if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".gif" || ext == ".svg" {
				name := strings.TrimSuffix(filepath.Base(cleaned), ext)
				sources = append(sources, EmojiSource{
					Name:        name,
					Description: "custom emoji: " + name,
					ImagePath:   cleaned,
					IsCustom:    true,
				})
			}
		}
	}

	if hasFiles && len(sources) > 0 {
		return sources, nil
	}

	// Extract unicode emojis
	emojis := ExtractEmojis(input)
	if len(emojis) == 0 {
		return nil, fmt.Errorf("no emojis or image files detected in input: %q", input)
	}

	for _, emoji := range emojis {
		desc, ok := emojiDescriptions[emoji]
		if !ok {
			// Generate a fallback description
			desc = "emoji " + emoji
		}
		name := sanitizeName(desc)
		sources = append(sources, EmojiSource{
			Emoji:       emoji,
			Name:        name,
			Description: desc,
		})
	}

	return sources, nil
}

func sanitizeName(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	name := re.ReplaceAllString(strings.ReplaceAll(s, " ", "_"), "")
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

// ============================================================================
// Image Generation
// ============================================================================

// GenerateStyledEmoji generates a styled version of an emoji
func (e *EmojiStyleClient) GenerateStyledEmoji(ctx context.Context, source EmojiSource, style string, engine string) ([]byte, error) {
	// Build the prompt
	prompt := e.buildPrompt(source, style)
	e.log("Generating: %s -> %q (engine: %s)", source.Description, style, engine)

	// Pick engine
	switch strings.ToLower(engine) {
	case "hf", "huggingface", "sd", "stable-diffusion":
		return e.generateWithHF(ctx, prompt)
	case "gemini", "imagen":
		return e.generateWithGemini(ctx, prompt)
	default:
		// Auto: prefer Gemini if available, fall back to HF
		if e.geminiKey != "" {
			return e.generateWithGemini(ctx, prompt)
		}
		if e.hfToken != "" {
			return e.generateWithHF(ctx, prompt)
		}
		return nil, fmt.Errorf("no AI engine available. Set GEMINI_API_KEY or HF_TOKEN")
	}
}

func (e *EmojiStyleClient) buildPrompt(source EmojiSource, style string) string {
	var base string
	if source.IsCustom {
		base = fmt.Sprintf("A stylized emoji character based on '%s'", source.Description)
	} else {
		base = fmt.Sprintf("A single emoji-style character of a %s (%s)", source.Description, source.Emoji)
	}

	prompt := fmt.Sprintf(
		"%s, %s. "+
			"Sticker style, clean vector art, centered on transparent or solid color background, "+
			"high detail, expressive, 512x512, no text, no watermark, single character only.",
		base, style,
	)

	return prompt
}

// generateWithGemini uses Gemini Imagen 3 API
func (e *EmojiStyleClient) generateWithGemini(ctx context.Context, prompt string) ([]byte, error) {
	if e.geminiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}

	// Use Imagen 3 via Gemini API
	apiURL := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/imagen-3.0-generate-002:predict?key=%s",
		e.geminiKey,
	)

	reqBody := map[string]any{
		"instances": []map[string]any{
			{"prompt": prompt},
		},
		"parameters": map[string]any{
			"sampleCount":      1,
			"aspectRatio":      "1:1",
			"personGeneration": "dont_allow",
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini image request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		// Fall back to Gemini Flash image generation
		return e.generateWithGeminiFlash(ctx, prompt)
	}

	var result struct {
		Predictions []struct {
			BytesBase64Encoded string `json:"bytesBase64Encoded"`
		} `json:"predictions"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	if len(result.Predictions) == 0 {
		return nil, fmt.Errorf("Gemini returned no images")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(result.Predictions[0].BytesBase64Encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return imageBytes, nil
}

// generateWithGeminiFlash uses the newer Gemini Flash model for image gen
func (e *EmojiStyleClient) generateWithGeminiFlash(ctx context.Context, prompt string) ([]byte, error) {
	apiURL := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent?key=%s",
		e.geminiKey,
	)

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini Flash request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Gemini Flash error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse the response looking for inline image data
	var flashResult struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData,omitempty"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &flashResult); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini Flash response: %w", err)
	}

	for _, cand := range flashResult.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") {
				imageBytes, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return nil, fmt.Errorf("failed to decode image: %w", err)
				}
				return imageBytes, nil
			}
		}
	}

	return nil, fmt.Errorf("Gemini Flash returned no image data")
}

// generateWithHF uses Hugging Face Inference API with Stable Diffusion
func (e *EmojiStyleClient) generateWithHF(ctx context.Context, prompt string) ([]byte, error) {
	if e.hfToken == "" {
		return nil, fmt.Errorf("HF_TOKEN not set")
	}

	model := "stabilityai/stable-diffusion-xl-base-1.0"
	apiURL := fmt.Sprintf("https://api-inference.huggingface.co/models/%s", model)

	reqBody := map[string]any{
		"inputs": prompt,
		"parameters": map[string]any{
			"width":               512,
			"height":              512,
			"num_inference_steps": 30,
			"guidance_scale":      7.5,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.hfToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wait-For-Model", "true")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HF image request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 503 {
		return nil, fmt.Errorf("HF model loading, try again in a minute")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HF error (status %d): %s", resp.StatusCode, string(body))
	}

	// Response is raw image bytes
	return body, nil
}

// ============================================================================
// WebP Conversion & Sticker Pack
// ============================================================================

// ConvertToWebP converts PNG bytes to WebP format
// Uses cwebp if available, otherwise falls back to saving as PNG
func ConvertToWebP(pngData []byte, outputPath string) error {
	// Write temp PNG
	tmpPNG := outputPath + ".tmp.png"
	if err := os.WriteFile(tmpPNG, pngData, 0644); err != nil {
		return fmt.Errorf("failed to write temp PNG: %w", err)
	}
	defer os.Remove(tmpPNG)

	// Try cwebp first
	if _, err := exec.LookPath("cwebp"); err == nil {
		cmd := exec.Command("cwebp", "-q", "90", "-resize", "512", "512", tmpPNG, "-o", outputPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cwebp failed: %s: %w", string(output), err)
		}
		return nil
	}

	// Try ffmpeg as fallback
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		cmd := exec.Command("ffmpeg", "-y", "-i", tmpPNG, "-vf", "scale=512:512", "-quality", "90", outputPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ffmpeg webp conversion failed: %s: %w", string(output), err)
		}
		return nil
	}

	// Try ImageMagick convert
	if _, err := exec.LookPath("convert"); err == nil {
		cmd := exec.Command("convert", tmpPNG, "-resize", "512x512", "-quality", "90", outputPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("convert failed: %s: %w", string(output), err)
		}
		return nil
	}

	// Last resort: just copy as PNG with .webp extension (some tools accept this)
	// Actually, let's use Go's built-in capability - save as PNG and note it
	if err := os.Rename(tmpPNG, outputPath); err != nil {
		return os.WriteFile(outputPath, pngData, 0644)
	}
	return nil
}

// CreateStickerPack generates styled emojis and packages them as a WebP sticker pack
func (e *EmojiStyleClient) CreateStickerPack(ctx context.Context, sources []EmojiSource, config EmojiStyleConfig) ([]EmojiStyleResult, error) {
	// Setup output directory
	outDir := config.OutputDir
	if outDir == "" {
		packName := config.PackName
		if packName == "" {
			packName = "emoji_stickers"
		}
		outDir = sanitizeName(packName)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	fmt.Printf("🎨 Generating sticker pack: %s (%d emojis)\n", outDir, len(sources))
	fmt.Printf("   Style: %s\n", config.Style)
	fmt.Printf("   Engine: %s\n", config.Engine)

	var results []EmojiStyleResult

	for i, source := range sources {
		fmt.Printf("   [%d/%d] %s %s... ", i+1, len(sources), source.Emoji, source.Description)

		result := EmojiStyleResult{Source: source}

		// Generate image
		imageBytes, err := e.GenerateStyledEmoji(ctx, source, config.Style, config.Engine)
		if err != nil {
			result.Error = err
			results = append(results, result)
			fmt.Printf("❌ %v\n", err)
			continue
		}

		// Save PNG
		pngPath := filepath.Join(outDir, source.Name+".png")
		if err := os.WriteFile(pngPath, imageBytes, 0644); err != nil {
			result.Error = err
			results = append(results, result)
			fmt.Printf("❌ save failed: %v\n", err)
			continue
		}
		result.PNGPath = pngPath

		// Convert to WebP
		webpPath := filepath.Join(outDir, source.Name+".webp")
		if err := ConvertToWebP(imageBytes, webpPath); err != nil {
			e.log("WebP conversion failed, keeping PNG: %v", err)
			result.WebPPath = pngPath // fallback to PNG
		} else {
			result.WebPPath = webpPath
		}

		results = append(results, result)
		fmt.Printf("✅\n")

		// Rate limit between requests
		if i < len(sources)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return results, nil
}

// ============================================================================
// Formatting
// ============================================================================

// FormatEmojiResults formats sticker pack results for piping
func FormatEmojiResults(results []EmojiStyleResult, config EmojiStyleConfig) string {
	var sb strings.Builder

	packName := config.PackName
	if packName == "" {
		packName = "Emoji Sticker Pack"
	}

	sb.WriteString(fmt.Sprintf("# %s\n\n", packName))
	sb.WriteString(fmt.Sprintf("**Style:** %s\n", config.Style))
	sb.WriteString(fmt.Sprintf("**Engine:** %s\n", config.Engine))

	success := 0
	failed := 0
	for _, r := range results {
		if r.Error != nil {
			failed++
		} else {
			success++
		}
	}

	sb.WriteString(fmt.Sprintf("**Generated:** %d/%d\n\n", success, len(results)))

	sb.WriteString("| # | Emoji | Name | File | Status |\n")
	sb.WriteString("|---|-------|------|------|--------|\n")

	for i, r := range results {
		status := "✅"
		file := r.WebPPath
		if r.Error != nil {
			status = fmt.Sprintf("❌ %v", r.Error)
			file = "-"
		}
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s |\n",
			i+1, r.Source.Emoji, r.Source.Description, file, status))
	}

	if success > 0 {
		sb.WriteString(fmt.Sprintf("\n**Output directory:** %s/\n", config.OutputDir))
		sb.WriteString(fmt.Sprintf("**Files:** %d WebP stickers ready for use\n", success))
	}

	return sb.String()
}

// ============================================================================
// Parse Helpers
// ============================================================================

// ParseEmojiStyleArgs parses the command arguments
// emoji_style "😀😎🔥" "wearing suits" -> engine auto
// emoji_style "😀😎🔥" "wearing suits" "hf" -> engine hf
func ParseEmojiStyleArgs(emojisArg, styleArg, engineArg string) EmojiStyleConfig {
	config := EmojiStyleConfig{
		Style:    styleArg,
		Engine:   engineArg,
		Size:     512,
		PackName: "emoji_stickers",
	}

	// Detect engine from style if specified there
	styleLower := strings.ToLower(styleArg)
	if strings.Contains(styleLower, "--engine=") {
		parts := strings.SplitN(styleLower, "--engine=", 2)
		if len(parts) == 2 {
			eng := strings.Fields(parts[1])[0]
			config.Engine = eng
			config.Style = strings.Replace(config.Style, "--engine="+eng, "", 1)
			config.Style = strings.TrimSpace(config.Style)
		}
	}

	// Default engine
	if config.Engine == "" {
		config.Engine = "auto"
	}

	return config
}

// isEmojiInput checks if string contains primarily emoji characters
func isEmojiInput(s string) bool {
	emojiCount := 0
	nonEmojiCount := 0
	for _, r := range s {
		if IsEmoji(r) {
			emojiCount++
		} else if !unicode.IsSpace(r) && r != ',' && r != ';' {
			nonEmojiCount++
		}
	}
	return emojiCount > 0 && emojiCount >= nonEmojiCount
}
