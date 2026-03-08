# Hugging Face Integration for AgentScript

## Overview

13 new `hf_*` commands give AgentScript access to **1,000+ open-source models** via the Hugging Face Inference API. One token, pure REST, no extra Go dependencies.

## Setup

```bash
# Get a free token at https://huggingface.co/settings/tokens
export HF_TOKEN="hf_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

Free tier includes generous rate limits for most models. PRO ($9/mo) unlocks larger models and higher throughput.

---

## New Commands (13)

| Command | Task | Default Model | Example |
|---------|------|---------------|---------|
| `hf_generate` | Text generation | Mistral-7B-Instruct | `hf_generate "explain monads"` |
| `hf_summarize` | Summarization | BART-large-CNN | `-> hf_summarize` |
| `hf_classify` | Sentiment/classification | DistilBERT-SST2 | `hf_classify "I love this!"` |
| `hf_ner` | Named entity recognition | BERT-NER | `hf_ner "Apple CEO Tim Cook..."` |
| `hf_translate` | Translation | Helsinki-NLP/opus-mt | `hf_translate "hello" "en" "fr"` |
| `hf_embeddings` | Text embeddings | all-MiniLM-L6-v2 | `hf_embeddings "semantic search"` |
| `hf_qa` | Question answering | RoBERTa-SQuAD2 | `hf_qa "who?" "context..."` |
| `hf_fill_mask` | Cloze/fill mask | BERT-base | `hf_fill_mask "Paris is [MASK]"` |
| `hf_zero_shot` | Zero-shot classification | BART-large-MNLI | `hf_zero_shot "text" "cat1,cat2"` |
| `hf_image_generate` | Text-to-image | SDXL | `hf_image_generate "sunset"` |
| `hf_image_classify` | Image classification | ViT-base | `hf_image_classify "photo.jpg"` |
| `hf_speech_to_text` | ASR / transcription | Whisper-large-v3 | `hf_speech_to_text "audio.mp3"` |
| `hf_similarity` | Sentence similarity | all-MiniLM-L6-v2 | `hf_similarity "a" "b,c,d"` |

---

## 1. grammar.go — Add new actions

Add to your Action regex:

```go
Action string `@("hf_generate"|"hf_summarize"|"hf_classify"|"hf_ner"|"hf_translate"|"hf_embeddings"|"hf_qa"|"hf_fill_mask"|"hf_zero_shot"|"hf_image_generate"|"hf_image_classify"|"hf_speech_to_text"|"hf_similarity"|...existing...)`
```

## 2. runtime.go — Add client and cases

### Add to Runtime struct

```go
type Runtime struct {
    // ... existing fields
    hf *HuggingFaceClient  // <-- NEW
}
```

### Initialize in NewRuntime

```go
hf: NewHuggingFaceClient(verbose),
```

### Add cases in executeCommand switch

```go
case "hf_generate":
    result, err = r.hfGenerate(ctx, cmd.Arg, cmd.Args, input)
case "hf_summarize":
    result, err = r.hfSummarize(ctx, cmd.Arg, input)
case "hf_classify":
    result, err = r.hfClassify(ctx, cmd.Arg, cmd.Args, input)
case "hf_ner":
    result, err = r.hfNER(ctx, cmd.Arg, cmd.Args, input)
case "hf_translate":
    result, err = r.hfTranslate(ctx, cmd.Arg, cmd.Args, input)
case "hf_embeddings":
    result, err = r.hfEmbeddings(ctx, cmd.Arg, cmd.Args, input)
case "hf_qa":
    result, err = r.hfQA(ctx, cmd.Arg, cmd.Args, input)
case "hf_fill_mask":
    result, err = r.hfFillMask(ctx, cmd.Arg, cmd.Args, input)
case "hf_zero_shot":
    result, err = r.hfZeroShot(ctx, cmd.Arg, cmd.Args, input)
case "hf_image_generate":
    result, err = r.hfImageGenerate(ctx, cmd.Arg, cmd.Args, input)
case "hf_similarity":
    result, err = r.hfSimilarity(ctx, cmd.Arg, cmd.Args, input)
```

### Handler methods

```go
func (r *Runtime) hfGenerate(ctx context.Context, arg string, args []string, input string) (string, error) {
    prompt := arg
    if prompt == "" {
        prompt = input
    }
    model := ""
    if len(args) > 0 {
        model = args[0]
    }
    return r.hf.TextGeneration(ctx, prompt, model, 500)
}

func (r *Runtime) hfSummarize(ctx context.Context, arg string, input string) (string, error) {
    text := input
    if text == "" {
        text = arg
    }
    return r.hf.Summarize(ctx, text, arg)
}

func (r *Runtime) hfClassify(ctx context.Context, arg string, args []string, input string) (string, error) {
    text := arg
    if text == "" {
        text = input
    }
    model := ""
    if len(args) > 0 {
        model = args[0]
    }
    results, err := r.hf.Classify(ctx, text, model)
    if err != nil {
        return "", err
    }
    return FormatClassification(results, text), nil
}

func (r *Runtime) hfNER(ctx context.Context, arg string, args []string, input string) (string, error) {
    text := arg
    if text == "" {
        text = input
    }
    model := ""
    if len(args) > 0 {
        model = args[0]
    }
    results, err := r.hf.NER(ctx, text, model)
    if err != nil {
        return "", err
    }
    return FormatNER(results, text), nil
}

func (r *Runtime) hfTranslate(ctx context.Context, arg string, args []string, input string) (string, error) {
    text := arg
    if text == "" {
        text = input
    }
    src := "en"
    tgt := "fr"
    if len(args) >= 2 {
        src = args[0]
        tgt = args[1]
    } else if len(args) >= 1 {
        tgt = args[0]
    }
    return r.hf.Translate(ctx, text, src, tgt)
}

func (r *Runtime) hfEmbeddings(ctx context.Context, arg string, args []string, input string) (string, error) {
    text := arg
    if text == "" {
        text = input
    }
    texts := strings.Split(text, "\n")
    model := ""
    if len(args) > 0 {
        model = args[0]
    }
    embeddings, err := r.hf.Embeddings(ctx, texts, model)
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("Generated %d embeddings of dimension %d", len(embeddings), len(embeddings[0])), nil
}

func (r *Runtime) hfQA(ctx context.Context, arg string, args []string, input string) (string, error) {
    question := arg
    context_text := input
    if len(args) > 0 {
        context_text = args[0]
    }
    model := ""
    if len(args) > 1 {
        model = args[1]
    }
    result, err := r.hf.QuestionAnswer(ctx, question, context_text, model)
    if err != nil {
        return "", err
    }
    return FormatQA(result, question), nil
}

func (r *Runtime) hfFillMask(ctx context.Context, arg string, args []string, input string) (string, error) {
    text := arg
    if text == "" {
        text = input
    }
    model := ""
    if len(args) > 0 {
        model = args[0]
    }
    results, err := r.hf.FillMask(ctx, text, model)
    if err != nil {
        return "", err
    }
    return FormatFillMask(results), nil
}

func (r *Runtime) hfZeroShot(ctx context.Context, arg string, args []string, input string) (string, error) {
    text := arg
    if text == "" {
        text = input
    }
    labels := []string{"positive", "negative", "neutral"}
    if len(args) > 0 {
        labels = strings.Split(args[0], ",")
        for i := range labels {
            labels[i] = strings.TrimSpace(labels[i])
        }
    }
    model := ""
    result, err := r.hf.ZeroShotClassify(ctx, text, labels, model)
    if err != nil {
        return "", err
    }
    return FormatZeroShot(result), nil
}

func (r *Runtime) hfImageGenerate(ctx context.Context, arg string, args []string, input string) (string, error) {
    prompt := arg
    if prompt == "" {
        prompt = input
    }
    model := ""
    if len(args) > 0 {
        model = args[0]
    }
    imageBytes, err := r.hf.TextToImage(ctx, prompt, model)
    if err != nil {
        return "", err
    }
    filename, err := SaveImage(imageBytes, "")
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("Image generated: %s (%d bytes)\nPrompt: %s", filename, len(imageBytes), prompt), nil
}

func (r *Runtime) hfSimilarity(ctx context.Context, arg string, args []string, input string) (string, error) {
    source := arg
    sentences := []string{}
    if len(args) > 0 {
        sentences = strings.Split(args[0], ",")
        for i := range sentences {
            sentences[i] = strings.TrimSpace(sentences[i])
        }
    }
    model := ""
    scores, err := r.hf.SentenceSimilarity(ctx, source, sentences, model)
    if err != nil {
        return "", err
    }
    return FormatSimilarity(source, sentences, scores), nil
}
```

---

## 3. DSL Usage Examples

### Sentiment Analysis
```bash
# Analyze sentiment of piped content
news "NVIDIA earnings" -> hf_classify
reddit "r/golang" -> hf_classify

# Use FinBERT for financial sentiment
stock "NVDA" -> hf_classify "ProsusAI/finbert"

# Analyze job descriptions
job_search "golang" -> foreach "section" -> hf_classify
```

### Named Entity Recognition
```bash
# Extract entities from news
news "Apple Google merger" -> hf_ner
# Output: Apple (ORG), Google (ORG), Tim Cook (PER), Mountain View (LOC)
```

### Zero-Shot Classification
```bash
# Classify without training
reddit "r/golang" -> hf_zero_shot "hiring,tutorial,question,showcase"

# Triage support tickets
read "tickets.txt" -> foreach "line" -> hf_zero_shot "bug,feature,question,billing"
```

### Translation
```bash
# Translate pipeline output
news "technology" -> hf_summarize -> hf_translate "en" "fr"
news "technology" -> hf_summarize -> hf_translate "en" "ja"
news "technology" -> hf_summarize -> hf_translate "en" "es"
```

### Question Answering
```bash
# Ask questions about piped content
search "kubernetes architecture" -> hf_qa "what is a pod?"
news "Federal Reserve" -> hf_qa "what did the Fed decide?"
```

### Summarization (Open Source)
```bash
# Use BART instead of Gemini for summarization
search "quantum computing" -> hf_summarize
rss "hn" -> hf_summarize
```

### Image Generation (Open Source)
```bash
# Use Stable Diffusion instead of Imagen
hf_image_generate "cyberpunk city at night, neon lights, rain"

# Use FLUX
hf_image_generate "robot in a garden" "black-forest-labs/FLUX.1-dev"
```

### Sentence Similarity
```bash
# Find which job matches best
hf_similarity "golang microservices developer" "backend engineer,frontend dev,devops,data scientist"
```

### Bring-Your-Own-Model
```bash
# Any HuggingFace model ID works as the model arg
hf_classify "text" "cardiffnlp/twitter-roberta-base-sentiment-latest"
hf_generate "prompt" "google/gemma-2-9b-it"
hf_summarize "text" "sshleifer/distilbart-cnn-12-6"
```

---

## 4. Power Pipelines

### Multilingual News Digest
```
parallel {
  news "AI" -> hf_summarize -> hf_translate "en" "fr"
  news "AI" -> hf_summarize -> hf_translate "en" "ja"
  news "AI" -> hf_summarize -> hf_translate "en" "es"
}
-> merge
-> doc_create "AI News - Multilingual"
-> email "team@company.com"
```

### Smart Job Triage
```
job_search "golang contract" "remote"
-> foreach "section"
-> hf_zero_shot "perfect_match,good_match,not_relevant"
-> if contains "perfect_match" { notify "slack" }
-> save "triaged-jobs.md"
```

### Sentiment-Aware Stock Monitor
```
parallel {
  stock "NVDA"
  news "NVIDIA" -> hf_classify "ProsusAI/finbert"
}
-> merge
-> ask "analyze NVIDIA: combine price action with news sentiment"
-> if contains "bearish" { notify "slack" }
```

### Entity Extraction Pipeline
```
news "tech acquisitions"
-> hf_ner
-> ask "list all companies mentioned and their relationships"
-> sheet_create "Tech M&A Entities"
```

### Content Moderation
```
reddit "r/golang" "new"
-> foreach "section"
-> hf_zero_shot "on_topic,spam,self_promotion,low_effort"
-> save "moderation-report.md"
```

---

## 5. Gemini vs HuggingFace: When to Use Which

| Need | Use | Why |
|------|-----|-----|
| General Q&A, chat | `ask` (Gemini) | Better reasoning |
| Summarization | Either | HF is free, Gemini is smarter |
| Sentiment analysis | `hf_classify` | Purpose-built, faster, free |
| NER | `hf_ner` | Specialized model, free |
| Translation | `hf_translate` | Dedicated models per language pair |
| Image generation | `image_generate` (Imagen) | Higher quality |
| Open-source images | `hf_image_generate` | SDXL/FLUX, no Google dependency |
| Zero-shot classify | `hf_zero_shot` | No training needed, free |
| Embeddings/search | `hf_embeddings` | Standard for RAG/search |
| Financial sentiment | `hf_classify "ProsusAI/finbert"` | Domain-specific model |

---

## 6. Updated Command Count: 58

| Category | Count | Commands |
|----------|-------|----------|
| Core | 8 | search, summarize, ask, analyze, save, read, list, merge |
| Google Workspace | 10 | email, calendar, meet, drive_save, doc_create, sheet_create, etc. |
| Multimodal | 5 | image_generate, image_analyze, video_generate, video_analyze, images_to_video |
| Data | 8 | job_search, weather, news, news_headlines, stock, crypto, reddit, rss |
| **Hugging Face** | **13** | **hf_generate, hf_summarize, hf_classify, hf_ner, hf_translate, hf_embeddings, hf_qa, hf_fill_mask, hf_zero_shot, hf_image_generate, hf_image_classify, hf_speech_to_text, hf_similarity** |
| Notifications | 3 | email, notify, whatsapp |
| Social | 1 | twitter |
| Control | 3 | parallel, if, foreach |
| Other | 7+ | translate, tts, filter, sort, stdin, etc. |

## Environment

```bash
# Just one new variable:
export HF_TOKEN="hf_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

Free. No credit card. 1,000+ models.
