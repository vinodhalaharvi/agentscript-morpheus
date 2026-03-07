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
	"strings"
	"time"
)

// ============================================================================
// Hugging Face Inference API Client for AgentScript
//
// Pure REST client — no external Go dependencies.
// Uses the Inference Providers (router) endpoint for broad model support.
//
// Supported tasks:
//   hf_generate         — Text generation / chat (Llama, Mistral, DeepSeek, Qwen, etc.)
//   hf_summarize        — Text summarization (DistilBART, BART, etc.)
//   hf_classify         — Text classification / sentiment (DistilBERT, FinBERT, etc.)
//   hf_ner              — Named entity recognition (BERT-NER, etc.)
//   hf_translate        — Translation (Helsinki-NLP, etc.)
//   hf_embeddings       — Text embeddings / feature extraction (sentence-transformers, etc.)
//   hf_qa               — Question answering (RoBERTa, DistilBERT, etc.)
//   hf_fill_mask        — Fill mask / cloze (BERT, RoBERTa, etc.)
//   hf_zero_shot        — Zero-shot classification (facebook/bart-large-mnli, etc.)
//   hf_image_generate   — Text-to-image (SDXL, FLUX, etc.)
//   hf_image_classify   — Image classification
//   hf_speech_to_text   — Automatic speech recognition (Whisper, etc.)
//   hf_similarity       — Sentence similarity
// ============================================================================

const (
	// Inference Providers router — routes to best available provider
	hfRouterURL = "https://router.huggingface.co"
	// Legacy direct inference endpoint (fallback)
	hfInferenceURL = "https://router.huggingface.co/hf-inference/models"
)

// HuggingFaceClient handles all Hugging Face API interactions
type HuggingFaceClient struct {
	token   string
	client  *http.Client
	verbose bool
}

// NewHuggingFaceClient creates a new HF client
func NewHuggingFaceClient(verbose bool) *HuggingFaceClient {
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		token = os.Getenv("HUGGINGFACE_TOKEN")
	}
	if token == "" {
		token = os.Getenv("HUGGINGFACEHUB_API_TOKEN")
	}

	return &HuggingFaceClient{
		token: token,
		client: &http.Client{
			Timeout: 120 * time.Second, // models can be slow to cold-start
		},
		verbose: verbose,
	}
}

func (hf *HuggingFaceClient) log(format string, args ...any) {
	if hf.verbose {
		fmt.Printf("[HF] "+format+"\n", args...)
	}
}

// IsConfigured returns true if a token is available
func (hf *HuggingFaceClient) IsConfigured() bool {
	return hf.token != ""
}

// ============================================================================
// Default models — battle-tested, reliable, free-tier friendly
// ============================================================================

var defaultModels = map[string]string{
	"generate":        "mistralai/Mistral-7B-Instruct-v0.3",
	"chat":            "meta-llama/Meta-Llama-3-8B-Instruct",
	"summarize":       "facebook/bart-large-cnn",
	"classify":        "distilbert/distilbert-base-uncased-finetuned-sst-2-english",
	"sentiment":       "distilbert/distilbert-base-uncased-finetuned-sst-2-english",
	"finance":         "ProsusAI/finbert",
	"ner":             "dslim/bert-base-NER",
	"translate_en_fr": "Helsinki-NLP/opus-mt-en-fr",
	"translate_en_es": "Helsinki-NLP/opus-mt-en-es",
	"translate_en_de": "Helsinki-NLP/opus-mt-en-de",
	"translate_en_ja": "Helsinki-NLP/opus-mt-en-ja",
	"translate_en_zh": "Helsinki-NLP/opus-mt-en-zh",
	"translate_fr_en": "Helsinki-NLP/opus-mt-fr-en",
	"translate_es_en": "Helsinki-NLP/opus-mt-es-en",
	"translate_de_en": "Helsinki-NLP/opus-mt-de-en",
	"embeddings":      "sentence-transformers/all-MiniLM-L6-v2",
	"qa":              "deepset/roberta-base-squad2",
	"fill_mask":       "google-bert/bert-base-uncased",
	"zero_shot":       "facebook/bart-large-mnli",
	"image":           "stabilityai/stable-diffusion-xl-base-1.0",
	"asr":             "openai/whisper-large-v3",
	"similarity":      "sentence-transformers/all-MiniLM-L6-v2",
	"image_classify":  "google/vit-base-patch16-224",
}

// resolveModel picks the right model for a task
func resolveModel(task string, userModel string) string {
	if userModel != "" {
		return userModel
	}
	if m, ok := defaultModels[task]; ok {
		return m
	}
	return ""
}

// ============================================================================
// Core HTTP caller
// ============================================================================

func (hf *HuggingFaceClient) callInference(ctx context.Context, model string, payload any) ([]byte, error) {
	if !hf.IsConfigured() {
		return nil, fmt.Errorf("Hugging Face token not set. Set HF_TOKEN env var (get one at https://huggingface.co/settings/tokens)")
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("%s/%s", hfInferenceURL, model)
	hf.log("POST %s (%d bytes)", apiURL, len(jsonBody))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+hf.token)
	req.Header.Set("Content-Type", "application/json")
	// Wait for model loading instead of 503
	req.Header.Set("X-Wait-For-Model", "true")

	resp, err := hf.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HF request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 503 {
		return nil, fmt.Errorf("model %s is loading. Retry in 20-60 seconds (status 503)", model)
	}
	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limited by Hugging Face. Free tier has limits. Wait and retry (status 429)")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HF error (status %d): %s", resp.StatusCode, truncateStr(string(body), 300))
	}

	return body, nil
}

// callInferenceRaw sends raw bytes (for binary inputs like audio/images)
func (hf *HuggingFaceClient) callInferenceRaw(ctx context.Context, model string, data []byte, contentType string) ([]byte, error) {
	if !hf.IsConfigured() {
		return nil, fmt.Errorf("HF_TOKEN not set")
	}

	apiURL := fmt.Sprintf("%s/%s", hfInferenceURL, model)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+hf.token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Wait-For-Model", "true")

	resp, err := hf.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HF error (status %d): %s", resp.StatusCode, truncateStr(string(body), 300))
	}

	return body, nil
}

// ============================================================================
// Task: Text Generation
// ============================================================================

func (hf *HuggingFaceClient) TextGeneration(ctx context.Context, prompt string, model string, maxTokens int) (string, error) {
	model = resolveModel("generate", model)
	if maxTokens <= 0 {
		maxTokens = 500
	}

	hf.log("Text generation with %s (%d max tokens)", model, maxTokens)

	payload := map[string]any{
		"inputs": prompt,
		"parameters": map[string]any{
			"max_new_tokens":   maxTokens,
			"temperature":      0.7,
			"return_full_text": false,
		},
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return "", err
	}

	// Response format: [{"generated_text": "..."}]
	var results []struct {
		GeneratedText string `json:"generated_text"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		// Try single object format
		var single struct {
			GeneratedText string `json:"generated_text"`
		}
		if err2 := json.Unmarshal(body, &single); err2 == nil {
			return single.GeneratedText, nil
		}
		return "", fmt.Errorf("failed to parse generation response: %w", err)
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no generated text returned")
	}

	return results[0].GeneratedText, nil
}

// ============================================================================
// Task: Summarization
// ============================================================================

func (hf *HuggingFaceClient) Summarize(ctx context.Context, text string, model string) (string, error) {
	model = resolveModel("summarize", model)
	hf.log("Summarizing with %s (%d chars)", model, len(text))

	// Truncate if too long for model context
	if len(text) > 10000 {
		text = text[:10000]
	}

	payload := map[string]any{
		"inputs": text,
		"parameters": map[string]any{
			"max_length": 300,
			"min_length": 50,
		},
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return "", err
	}

	var results []struct {
		SummaryText string `json:"summary_text"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return "", fmt.Errorf("failed to parse summary: %w", err)
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no summary returned")
	}

	return results[0].SummaryText, nil
}

// ============================================================================
// Task: Text Classification / Sentiment Analysis
// ============================================================================

type ClassificationResult struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

func (hf *HuggingFaceClient) Classify(ctx context.Context, text string, model string) ([]ClassificationResult, error) {
	model = resolveModel("classify", model)
	hf.log("Classifying with %s", model)

	payload := map[string]any{
		"inputs": text,
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	// Response: [[{"label":"POSITIVE","score":0.99},...]]
	var nested [][]ClassificationResult
	if err := json.Unmarshal(body, &nested); err != nil {
		// Try flat array
		var flat []ClassificationResult
		if err2 := json.Unmarshal(body, &flat); err2 != nil {
			return nil, fmt.Errorf("failed to parse classification: %w", err)
		}
		return flat, nil
	}

	if len(nested) > 0 {
		return nested[0], nil
	}
	return nil, fmt.Errorf("no classification results")
}

// ============================================================================
// Task: Named Entity Recognition
// ============================================================================

type NERResult struct {
	EntityGroup string  `json:"entity_group"`
	Word        string  `json:"word"`
	Score       float64 `json:"score"`
	Start       int     `json:"start"`
	End         int     `json:"end"`
}

func (hf *HuggingFaceClient) NER(ctx context.Context, text string, model string) ([]NERResult, error) {
	model = resolveModel("ner", model)
	hf.log("NER with %s", model)

	payload := map[string]any{
		"inputs": text,
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	var results []NERResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// ============================================================================
// Task: Translation
// ============================================================================

func (hf *HuggingFaceClient) Translate(ctx context.Context, text string, sourceLang string, targetLang string) (string, error) {
	// Build translation model key
	modelKey := fmt.Sprintf("translate_%s_%s", strings.ToLower(sourceLang), strings.ToLower(targetLang))
	model := resolveModel(modelKey, "")

	if model == "" {
		// Try Helsinki-NLP pattern
		model = fmt.Sprintf("Helsinki-NLP/opus-mt-%s-%s", strings.ToLower(sourceLang), strings.ToLower(targetLang))
	}

	hf.log("Translating %s->%s with %s", sourceLang, targetLang, model)

	payload := map[string]any{
		"inputs": text,
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return "", err
	}

	var results []struct {
		TranslationText string `json:"translation_text"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no translation returned")
	}

	return results[0].TranslationText, nil
}

// ============================================================================
// Task: Embeddings / Feature Extraction
// ============================================================================

func (hf *HuggingFaceClient) Embeddings(ctx context.Context, texts []string, model string) ([][]float64, error) {
	model = resolveModel("embeddings", model)
	hf.log("Embeddings with %s (%d texts)", model, len(texts))

	payload := map[string]any{
		"inputs": texts,
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	var embeddings [][]float64
	if err := json.Unmarshal(body, &embeddings); err != nil {
		return nil, err
	}

	return embeddings, nil
}

// ============================================================================
// Task: Question Answering
// ============================================================================

type QAResult struct {
	Answer string  `json:"answer"`
	Score  float64 `json:"score"`
	Start  int     `json:"start"`
	End    int     `json:"end"`
}

func (hf *HuggingFaceClient) QuestionAnswer(ctx context.Context, question string, contextText string, model string) (*QAResult, error) {
	model = resolveModel("qa", model)
	hf.log("QA with %s", model)

	payload := map[string]any{
		"inputs": map[string]string{
			"question": question,
			"context":  contextText,
		},
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	var result QAResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ============================================================================
// Task: Zero-Shot Classification
// ============================================================================

type ZeroShotResult struct {
	Sequence string    `json:"sequence"`
	Labels   []string  `json:"labels"`
	Scores   []float64 `json:"scores"`
}

func (hf *HuggingFaceClient) ZeroShotClassify(ctx context.Context, text string, labels []string, model string) (*ZeroShotResult, error) {
	model = resolveModel("zero_shot", model)
	hf.log("Zero-shot with %s, labels: %v", model, labels)

	payload := map[string]any{
		"inputs": text,
		"parameters": map[string]any{
			"candidate_labels": labels,
		},
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	var result ZeroShotResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ============================================================================
// Task: Fill Mask
// ============================================================================

type FillMaskResult struct {
	Sequence string  `json:"sequence"`
	Score    float64 `json:"score"`
	Token    int     `json:"token"`
	TokenStr string  `json:"token_str"`
}

func (hf *HuggingFaceClient) FillMask(ctx context.Context, text string, model string) ([]FillMaskResult, error) {
	model = resolveModel("fill_mask", model)
	hf.log("Fill mask with %s", model)

	payload := map[string]any{
		"inputs": text,
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	var results []FillMaskResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// ============================================================================
// Task: Text-to-Image
// ============================================================================

func (hf *HuggingFaceClient) TextToImage(ctx context.Context, prompt string, model string) ([]byte, error) {
	model = resolveModel("image", model)
	hf.log("Image generation with %s: %q", model, truncateStr(prompt, 60))

	payload := map[string]any{
		"inputs": prompt,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("%s/%s", hfInferenceURL, model)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+hf.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wait-For-Model", "true")

	resp, err := hf.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Check if it's an error response (JSON)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HF image error (status %d): %s", resp.StatusCode, truncateStr(string(body), 300))
	}

	// Check content type — should be image bytes
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		return nil, fmt.Errorf("unexpected JSON response: %s", truncateStr(string(body), 200))
	}

	return body, nil
}

// ============================================================================
// Task: Sentence Similarity
// ============================================================================

func (hf *HuggingFaceClient) SentenceSimilarity(ctx context.Context, source string, sentences []string, model string) ([]float64, error) {
	model = resolveModel("similarity", model)
	hf.log("Similarity with %s", model)

	payload := map[string]any{
		"inputs": map[string]any{
			"source_sentence": source,
			"sentences":       sentences,
		},
	}

	body, err := hf.callInference(ctx, model, payload)
	if err != nil {
		return nil, err
	}

	var scores []float64
	if err := json.Unmarshal(body, &scores); err != nil {
		return nil, err
	}

	return scores, nil
}

// ============================================================================
// Formatting Functions
// ============================================================================

func FormatClassification(results []ClassificationResult, text string) string {
	if len(results) == 0 {
		return "No classification results."
	}

	var sb strings.Builder
	sb.WriteString("## Classification Results\n\n")
	sb.WriteString(fmt.Sprintf("**Input:** %s\n\n", truncateStr(text, 100)))
	sb.WriteString("| Label | Confidence |\n")
	sb.WriteString("|-------|------------|\n")
	for _, r := range results {
		bar := strings.Repeat("█", int(r.Score*20))
		sb.WriteString(fmt.Sprintf("| %s | %.1f%% %s |\n", r.Label, r.Score*100, bar))
	}
	return sb.String()
}

func FormatNER(results []NERResult, text string) string {
	if len(results) == 0 {
		return "No named entities found."
	}

	var sb strings.Builder
	sb.WriteString("## Named Entity Recognition\n\n")
	sb.WriteString(fmt.Sprintf("**Input:** %s\n\n", truncateStr(text, 200)))
	sb.WriteString("| Entity | Type | Confidence |\n")
	sb.WriteString("|--------|------|------------|\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("| %s | %s | %.1f%% |\n", r.Word, r.EntityGroup, r.Score*100))
	}
	return sb.String()
}

func FormatZeroShot(result *ZeroShotResult) string {
	if result == nil {
		return "No classification results."
	}

	var sb strings.Builder
	sb.WriteString("## Zero-Shot Classification\n\n")
	sb.WriteString(fmt.Sprintf("**Input:** %s\n\n", truncateStr(result.Sequence, 100)))
	sb.WriteString("| Label | Confidence |\n")
	sb.WriteString("|-------|------------|\n")
	for i, label := range result.Labels {
		if i < len(result.Scores) {
			bar := strings.Repeat("█", int(result.Scores[i]*20))
			sb.WriteString(fmt.Sprintf("| %s | %.1f%% %s |\n", label, result.Scores[i]*100, bar))
		}
	}
	return sb.String()
}

func FormatFillMask(results []FillMaskResult) string {
	if len(results) == 0 {
		return "No fill mask results."
	}

	var sb strings.Builder
	sb.WriteString("## Fill Mask Predictions\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. **%s** (%.1f%%) → %s\n", i+1, r.TokenStr, r.Score*100, r.Sequence))
	}
	return sb.String()
}

func FormatQA(result *QAResult, question string) string {
	if result == nil {
		return "No answer found."
	}

	return fmt.Sprintf("## Question Answering\n\n**Q:** %s\n**A:** %s\n**Confidence:** %.1f%%\n",
		question, result.Answer, result.Score*100)
}

func FormatSimilarity(source string, sentences []string, scores []float64) string {
	var sb strings.Builder
	sb.WriteString("## Sentence Similarity\n\n")
	sb.WriteString(fmt.Sprintf("**Source:** %s\n\n", source))
	sb.WriteString("| Sentence | Similarity |\n")
	sb.WriteString("|----------|------------|\n")
	for i, sent := range sentences {
		if i < len(scores) {
			bar := strings.Repeat("█", int(scores[i]*20))
			sb.WriteString(fmt.Sprintf("| %s | %.1f%% %s |\n", truncateStr(sent, 60), scores[i]*100, bar))
		}
	}
	return sb.String()
}

func FormatImageBase64(imageBytes []byte, prompt string) string {
	encoded := base64.StdEncoding.EncodeToString(imageBytes)
	return fmt.Sprintf("## Generated Image\n\n**Prompt:** %s\n\n[Image: %d bytes, base64-encoded]\n\nBase64: %s\n",
		prompt, len(imageBytes), truncateStr(encoded, 100)+"...")
}

// ============================================================================
// DSL Argument Parsing
// ============================================================================

// ParseHFArgs parses arguments for HF commands
// hf_classify "text"                            → default model
// hf_classify "text" "ProsusAI/finbert"          → specific model
// hf_translate "text" "en" "fr"                  → language pair
// hf_zero_shot "text" "label1,label2,label3"     → with labels
// hf_qa "question" "context"                     → Q&A pair
func ParseHFArgs(task string, args []string) (text string, model string, extra map[string]string) {
	extra = make(map[string]string)

	if len(args) == 0 {
		return "", "", extra
	}

	text = args[0]

	switch task {
	case "hf_translate":
		if len(args) >= 3 {
			extra["source_lang"] = args[1]
			extra["target_lang"] = args[2]
		} else if len(args) >= 2 {
			extra["source_lang"] = "en"
			extra["target_lang"] = args[1]
		}

	case "hf_zero_shot":
		if len(args) >= 2 {
			extra["labels"] = args[1]
		}

	case "hf_qa":
		if len(args) >= 2 {
			extra["context"] = args[1]
		}

	case "hf_similarity":
		if len(args) >= 2 {
			extra["sentences"] = args[1]
		}

	default:
		if len(args) >= 2 {
			model = args[1]
		}
	}

	return text, model, extra
}

// ============================================================================
// Helper
// ============================================================================

func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

// SaveImage saves image bytes to a file and returns the path
func SaveImage(imageBytes []byte, filename string) (string, error) {
	if filename == "" {
		filename = fmt.Sprintf("hf_image_%d.png", time.Now().Unix())
	}
	if err := os.WriteFile(filename, imageBytes, 0644); err != nil {
		return "", err
	}
	return filename, nil
}
