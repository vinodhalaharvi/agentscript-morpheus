package huggingface

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps HuggingFaceClient as a Plugin.
type Plugin struct {
	client *HuggingFaceClient
}

// NewPlugin creates a huggingface plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{client: NewHuggingFaceClient(verbose)}
}

func (p *Plugin) Name() string { return "huggingface" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"hf_generate":       p.generate,
		"hf_summarize":      p.summarize,
		"hf_classify":       p.classify,
		"hf_ner":            p.ner,
		"hf_translate":      p.translate,
		"hf_embeddings":     p.embeddings,
		"hf_qa":             p.qa,
		"hf_fill_mask":      p.fillMask,
		"hf_zero_shot":      p.zeroShot,
		"hf_image_generate": p.imageGenerate,
		"hf_image_classify": p.imageClassify,
		"hf_speech_to_text": p.speechToText,
		"hf_similarity":     p.similarity,
	}
}

func (p *Plugin) generate(ctx context.Context, args []string, input string) (string, error) {
	prompt := plugin.Coalesce(args, 0, input)
	model := plugin.Arg(args, 1)
	return p.client.TextGeneration(ctx, prompt, model, 500)
}

func (p *Plugin) summarize(ctx context.Context, args []string, input string) (string, error) {
	text := input
	model := plugin.Arg(args, 0)
	if text == "" {
		text = model
		model = ""
	}
	return p.client.Summarize(ctx, text, model)
}

func (p *Plugin) classify(ctx context.Context, args []string, input string) (string, error) {
	text := plugin.Coalesce(args, 0, input)
	model := plugin.Arg(args, 1)
	results, err := p.client.Classify(ctx, text, model)
	if err != nil {
		return "", err
	}
	return FormatClassification(results, text), nil
}

func (p *Plugin) ner(ctx context.Context, args []string, input string) (string, error) {
	text := plugin.Coalesce(args, 0, input)
	model := plugin.Arg(args, 1)
	results, err := p.client.NER(ctx, text, model)
	if err != nil {
		return "", err
	}
	return FormatNER(results, text), nil
}

func (p *Plugin) translate(ctx context.Context, args []string, input string) (string, error) {
	text := plugin.Coalesce(args, 0, input)
	srcLang := plugin.Arg(args, 1)
	tgtLang := plugin.Arg(args, 2)
	if srcLang == "" {
		srcLang = "en"
	}
	if tgtLang == "" {
		tgtLang = "fr"
	}
	return p.client.Translate(ctx, text, srcLang, tgtLang)
}

func (p *Plugin) embeddings(ctx context.Context, args []string, input string) (string, error) {
	text := plugin.Coalesce(args, 0, input)
	model := plugin.Arg(args, 1)
	texts := strings.Split(text, "\n")
	embeddings, err := p.client.Embeddings(ctx, texts, model)
	if err != nil {
		return "", err
	}
	if len(embeddings) > 0 {
		return fmt.Sprintf("Generated %d embeddings of dimension %d", len(embeddings), len(embeddings[0])), nil
	}
	return "No embeddings generated", nil
}

func (p *Plugin) qa(ctx context.Context, args []string, input string) (string, error) {
	question := plugin.Arg(args, 0)
	contextText := plugin.Coalesce(args, 1, input)
	result, err := p.client.QuestionAnswer(ctx, question, contextText, "")
	if err != nil {
		return "", err
	}
	return FormatQA(result, question), nil
}

func (p *Plugin) fillMask(ctx context.Context, args []string, input string) (string, error) {
	text := plugin.Coalesce(args, 0, input)
	model := plugin.Arg(args, 1)
	results, err := p.client.FillMask(ctx, text, model)
	if err != nil {
		return "", err
	}
	return FormatFillMask(results), nil
}

func (p *Plugin) zeroShot(ctx context.Context, args []string, input string) (string, error) {
	text := plugin.Coalesce(args, 0, input)
	labelsStr := plugin.Arg(args, 1)
	labels := []string{"positive", "negative", "neutral"}
	if labelsStr != "" {
		labels = strings.Split(labelsStr, ",")
		for i := range labels {
			labels[i] = strings.TrimSpace(labels[i])
		}
	}
	result, err := p.client.ZeroShotClassify(ctx, text, labels, "")
	if err != nil {
		return "", err
	}
	return FormatZeroShot(result), nil
}

func (p *Plugin) imageGenerate(ctx context.Context, args []string, input string) (string, error) {
	prompt := plugin.Coalesce(args, 0, input)
	model := plugin.Arg(args, 1)
	imageBytes, err := p.client.TextToImage(ctx, prompt, model)
	if err != nil {
		return "", err
	}
	filename, err := SaveImage(imageBytes, "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Image generated: %s (%d bytes)\nPrompt: %s", filename, len(imageBytes), prompt), nil
}

func (p *Plugin) imageClassify(ctx context.Context, args []string, input string) (string, error) {
	imagePath := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image %s: %w", imagePath, err)
	}
	body, err := p.client.CallInferenceRaw(ctx, ResolveModel("image_classify", ""), data, "image/jpeg")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (p *Plugin) speechToText(ctx context.Context, args []string, input string) (string, error) {
	audioPath := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio %s: %w", audioPath, err)
	}
	body, err := p.client.CallInferenceRaw(ctx, ResolveModel("asr", ""), data, "audio/flac")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (p *Plugin) similarity(ctx context.Context, args []string, input string) (string, error) {
	source := plugin.Coalesce(args, 0, input)
	sentencesStr := plugin.Arg(args, 1)
	var sentences []string
	if sentencesStr != "" {
		sentences = strings.Split(sentencesStr, ",")
		for i := range sentences {
			sentences[i] = strings.TrimSpace(sentences[i])
		}
	}
	scores, err := p.client.SentenceSimilarity(ctx, source, sentences, "")
	if err != nil {
		return "", err
	}
	return FormatSimilarity(source, sentences, scores), nil
}
