package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Runtime executes AgentScript commands
type Runtime struct {
	gemini    *GeminiClient
	google    *GoogleClient
	github    *GitHubClient
	claude    *ClaudeClient
	mcp       *MCPClient
	hf        *HuggingFaceClient
	crypto    *CryptoClient
	reddit    *RedditClient
	rss       *RSSClient
	notifier  *NotifyClient
	cache     *Cache
	twitter   *TwitterClient
	whatsapp  *WhatsAppClient
	emoji     *EmojiStyleClient
	verbose   bool
	searchKey string
	vars      map[string]string
}

// RuntimeConfig holds runtime configuration
type RuntimeConfig struct {
	GeminiAPIKey       string
	ClaudeAPIKey       string
	SearchAPIKey       string
	Model              string
	Verbose            bool
	GoogleCredsFile    string
	GoogleTokenFile    string
	GitHubClientID     string
	GitHubClientSecret string
	GitHubTokenFile    string
}

// NewRuntime creates a new Runtime instance
func NewRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	var geminiClient *GeminiClient
	if cfg.GeminiAPIKey != "" {
		geminiClient = NewGeminiClient(cfg.GeminiAPIKey, cfg.Model)
	}

	var claudeClient *ClaudeClient
	if cfg.ClaudeAPIKey != "" {
		claudeClient = NewClaudeClient(cfg.ClaudeAPIKey)
	}

	var googleClient *GoogleClient
	if cfg.GoogleCredsFile != "" {
		tokenFile := cfg.GoogleTokenFile
		if tokenFile == "" {
			tokenFile = "token.json"
		}
		var err error
		googleClient, err = NewGoogleClient(ctx, cfg.GoogleCredsFile, tokenFile)
		if err != nil {
			// Don't fail, just log warning
			fmt.Fprintf(os.Stderr, "Warning: Google API not available: %v\n", err)
		}
	}

	var githubClient *GitHubClient
	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		tokenFile := cfg.GitHubTokenFile
		if tokenFile == "" {
			tokenFile = "github_token.json"
		}
		var err error
		githubClient, err = NewGitHubClient(ctx, cfg.GitHubClientID, cfg.GitHubClientSecret, tokenFile)
		if err != nil {
			// Don't fail, just log warning
			fmt.Fprintf(os.Stderr, "Warning: GitHub API not available: %v\n", err)
		}
	}

	return &Runtime{
		gemini:    geminiClient,
		google:    googleClient,
		github:    githubClient,
		claude:    claudeClient,
		mcp:       NewMCPClient(),
		hf:        NewHuggingFaceClient(cfg.Verbose),
		crypto:    NewCryptoClient(cfg.Verbose),
		reddit:    NewRedditClient(cfg.Verbose),
		rss:       NewRSSClient(cfg.Verbose),
		notifier:  NewNotifyClient(cfg.Verbose),
		cache:     NewCache(cfg.Verbose),
		twitter:   NewTwitterClient(cfg.Verbose),
		whatsapp:  NewWhatsAppClient(cfg.Verbose),
		emoji:     NewEmojiStyleClient(cfg.Verbose),
		verbose:   cfg.Verbose,
		searchKey: cfg.SearchAPIKey,
		vars:      make(map[string]string),
	}, nil
}

// Execute runs a parsed program
func (r *Runtime) Execute(ctx context.Context, program *Program) (string, error) {
	var result string
	for _, stmt := range program.Statements {
		var err error
		result, err = r.executeStatement(ctx, stmt, result)
		if err != nil {
			return "", err
		}
	}
	return result, nil
}

// executeStatement executes a statement (command or parallel block)
func (r *Runtime) executeStatement(ctx context.Context, stmt *Statement, input string) (string, error) {
	var result string
	var err error

	if stmt.Parallel != nil {
		result, err = r.executeParallel(ctx, stmt.Parallel, input)
	} else if stmt.Command != nil {
		result, err = r.executeCommand(ctx, stmt.Command, input)
	}

	if err != nil {
		return "", err
	}

	// Follow the pipe chain
	if stmt.Pipe != nil {
		return r.executeStatement(ctx, stmt.Pipe, result)
	}

	return result, nil
}

// executeParallel runs multiple branches concurrently
func (r *Runtime) executeParallel(ctx context.Context, parallel *Parallel, input string) (string, error) {
	r.log("Executing PARALLEL with %d branches", len(parallel.Branches))

	// Results from each branch
	type branchResult struct {
		index  int
		result string
		err    error
	}

	results := make(chan branchResult, len(parallel.Branches))
	var wg sync.WaitGroup

	// Launch all branches concurrently
	for i, branch := range parallel.Branches {
		wg.Add(1)
		go func(idx int, stmt *Statement) {
			defer wg.Done()
			res, err := r.executeStatement(ctx, stmt, input)
			results <- branchResult{index: idx, result: res, err: err}
		}(i, branch)
	}

	// Wait for all branches to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results in order
	orderedResults := make([]string, len(parallel.Branches))
	for res := range results {
		if res.err != nil {
			return "", fmt.Errorf("parallel branch %d failed: %w", res.index, res.err)
		}
		orderedResults[res.index] = res.result
	}

	// Combine results with clear separators
	var combined []string
	for i, res := range orderedResults {
		combined = append(combined, fmt.Sprintf("=== Branch %d ===\n%s", i+1, res))
	}

	r.log("PARALLEL complete: %d branches finished", len(parallel.Branches))
	return strings.Join(combined, "\n\n"), nil
}

// executeCommand executes a single command
func (r *Runtime) executeCommand(ctx context.Context, cmd *Command, input string) (string, error) {
	r.log("Executing: %s %q (input: %d bytes)", cmd.Action, cmd.Arg, len(input))

	var result string
	var err error

	switch cmd.Action {
	case "search":
		result, err = r.search(ctx, cmd.Arg)
	case "summarize":
		result, err = r.geminiCall(ctx, "Summarize the following content concisely:\n\n"+input)
	case "ask":
		prompt := cmd.Arg
		if input != "" {
			prompt = cmd.Arg + "\n\nContext:\n" + input
		}
		result, err = r.geminiCall(ctx, prompt)
	case "analyze":
		prompt := "Analyze the following"
		if cmd.Arg != "" {
			prompt += " focusing on " + cmd.Arg
		}
		prompt += ":\n\n" + input
		result, err = r.geminiCall(ctx, prompt)
	case "save":
		result, err = r.save(cmd.Arg, input)
	case "read":
		result, err = r.read(cmd.Arg)
	case "stdin":
		result, err = r.readStdin(cmd.Arg)
	case "list":
		result, err = r.list(cmd.Arg)
	case "merge":
		// merge just passes through the input - it's used after parallel
		// to signal that we want to combine results (which parallel already does)
		result = input
	case "email":
		result, err = r.email(ctx, cmd.Arg, input)
	case "calendar":
		result, err = r.calendar(ctx, cmd.Arg, input)
	case "meet":
		result, err = r.meet(ctx, cmd.Arg, input)
	case "drive_save":
		result, err = r.driveSave(ctx, cmd.Arg, input)
	case "doc_create":
		result, err = r.docCreate(ctx, cmd.Arg, input)
	case "sheet_append":
		result, err = r.sheetAppend(ctx, cmd.Arg, input)
	case "sheet_create":
		result, err = r.sheetCreate(ctx, cmd.Arg, input)
	case "task":
		result, err = r.task(ctx, cmd.Arg, input)
	case "contact_find":
		result, err = r.contactFind(ctx, cmd.Arg)
	case "youtube_search":
		result, err = r.youtubeSearch(ctx, cmd.Arg)
	case "youtube_upload":
		result, err = r.youtubeUpload(ctx, cmd.Arg, input, false)
	case "youtube_shorts":
		result, err = r.youtubeUpload(ctx, cmd.Arg, input, true)
	case "image_generate":
		result, err = r.imageGenerate(ctx, cmd.Arg, input)
	case "image_analyze":
		result, err = r.imageAnalyze(ctx, cmd.Arg, input)
	case "video_analyze":
		result, err = r.videoAnalyze(ctx, cmd.Arg, input)
	case "video_generate":
		result, err = r.videoGenerate(ctx, cmd.Arg, input)
	case "images_to_video":
		result, err = r.imagesToVideo(ctx, cmd.Arg, input)
	case "text_to_speech":
		result, err = r.textToSpeech(ctx, cmd.Arg, input)
	case "audio_video_merge":
		result, err = r.audioVideoMerge(ctx, cmd.Arg, input)
	case "image_audio_merge":
		result, err = r.imageAudioMerge(ctx, cmd.Arg, input)
	case "maps_trip":
		result, err = r.mapsTrip(ctx, cmd.Arg, input)
	case "form_create":
		result, err = r.formCreate(ctx, cmd.Arg, input)
	case "form_responses":
		result, err = r.formResponses(ctx, cmd.Arg, input)
	case "translate":
		result, err = r.translate(ctx, cmd.Arg, input)
	case "places_search":
		result, err = r.placesSearch(ctx, cmd.Arg, input)
	case "mcp_connect":
		result, err = r.mcpConnect(ctx, cmd.Arg, cmd.Arg2)
	case "mcp_list":
		result, err = r.mcpList(ctx, cmd.Arg, input)
	case "mcp":
		result, err = r.mcpCall(ctx, cmd.Arg, cmd.Arg2)
	case "video_script":
		result, err = r.videoScript(ctx, cmd.Arg, input)
	case "confirm":
		result, err = r.confirm(ctx, cmd.Arg, input)
	case "github_pages":
		result, err = r.githubPages(ctx, cmd.Arg, input)
	case "github_pages_html":
		result, err = r.githubPagesHTML(ctx, cmd.Arg, input)

	// ==================== Data Commands ====================
	case "job_search":
		result, err = r.jobSearchCmd(ctx, cmd.Arg, cmd.Arg2, cmd.Arg3, input)
	case "weather":
		result, err = r.weatherFetch(ctx, cmd.Arg, input)
	case "news":
		result, err = r.newsFetch(ctx, cmd.Arg, input)
	case "news_headlines":
		result, err = r.newsHeadlines(ctx, cmd.Arg, input)
	case "stock":
		result, err = r.stockFetch(ctx, cmd.Arg, input)
	case "crypto":
		result, err = r.cryptoFetch(ctx, cmd.Arg, input)
	case "reddit":
		result, err = r.redditFetch(ctx, cmd.Arg, cmd.Arg2, input)
	case "rss":
		result, err = r.rssFetch(ctx, cmd.Arg, input)
	case "twitter":
		result, err = r.twitterFetch(ctx, cmd.Arg, input)

	// ==================== Notifications ====================
	case "notify":
		result, err = r.notifyCmd(ctx, cmd.Arg, input)
	case "whatsapp":
		result, err = r.whatsappSend(ctx, cmd.Arg, input)

	// ==================== Control Flow ====================
	case "foreach":
		result, err = r.foreachCmd(ctx, cmd.Arg, input)
	case "if":
		result, err = r.ifCmd(ctx, cmd.Arg, input)

	// ==================== Hugging Face ====================
	case "hf_generate":
		result, err = r.hfGenerate(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_summarize":
		result, err = r.hfSummarize(ctx, cmd.Arg, input)
	case "hf_classify":
		result, err = r.hfClassify(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_ner":
		result, err = r.hfNER(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_translate":
		result, err = r.hfTranslateCmd(ctx, cmd.Arg, cmd.Arg2, cmd.Arg3, input)
	case "hf_embeddings":
		result, err = r.hfEmbeddings(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_qa":
		result, err = r.hfQA(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_fill_mask":
		result, err = r.hfFillMask(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_zero_shot":
		result, err = r.hfZeroShot(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_image_generate":
		result, err = r.hfImageGen(ctx, cmd.Arg, cmd.Arg2, input)
	case "hf_image_classify":
		result, err = r.hfImageClassify(ctx, cmd.Arg, input)
	case "hf_speech_to_text":
		result, err = r.hfSpeechToText(ctx, cmd.Arg, input)
	case "hf_similarity":
		result, err = r.hfSimilarityCmd(ctx, cmd.Arg, cmd.Arg2, input)

	// ==================== Emoji Style ====================
	case "emoji_style":
		result, err = r.emojiStyleCmd(ctx, cmd.Arg, cmd.Arg2, cmd.Arg3, input)

	default:
		err = fmt.Errorf("unknown action: %s", cmd.Action)
	}

	if err != nil {
		return "", fmt.Errorf("%s failed: %w", cmd.Action, err)
	}

	r.log("Result: %d bytes", len(result))
	return result, nil
}

// geminiCall makes a call to the Gemini API
func (r *Runtime) geminiCall(ctx context.Context, prompt string) (string, error) {
	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY not set - required for this command")
	}
	return r.gemini.GenerateContent(ctx, prompt)
}

// search performs a web search
func (r *Runtime) search(ctx context.Context, query string) (string, error) {
	// If no search API key, use Gemini to generate a response
	if r.searchKey == "" {
		return r.geminiCall(ctx, "Please provide information about: "+query)
	}

	// Use SerpAPI or similar
	searchURL := fmt.Sprintf(
		"https://serpapi.com/search.json?q=%s&api_key=%s",
		url.QueryEscape(query),
		r.searchKey,
	)

	resp, err := http.Get(searchURL)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read search response: %w", err)
	}

	// Parse and extract relevant snippets
	var searchResult map[string]any
	if err := json.Unmarshal(body, &searchResult); err != nil {
		return "", fmt.Errorf("failed to parse search response: %w", err)
	}

	// Extract organic results
	var snippets []string
	if organic, ok := searchResult["organic_results"].([]any); ok {
		for i, item := range organic {
			if i >= 5 {
				break
			}
			if result, ok := item.(map[string]any); ok {
				title := result["title"]
				snippet := result["snippet"]
				link := result["link"]
				snippets = append(snippets, fmt.Sprintf("- %v\n  %v\n  %v", title, snippet, link))
			}
		}
	}

	if len(snippets) == 0 {
		return string(body), nil
	}

	return strings.Join(snippets, "\n\n"), nil
}

// save writes content to a file
func (r *Runtime) save(path, content string) (string, error) {
	// Check if content is a temp image file from imageGenerate
	if strings.HasPrefix(content, "IMAGEFILE:") {
		tempPath := strings.TrimPrefix(content, "IMAGEFILE:")
		// Move temp file to final destination
		if err := os.Rename(tempPath, path); err != nil {
			// If rename fails (cross-device), try copy
			data, err := os.ReadFile(tempPath)
			if err != nil {
				return "", fmt.Errorf("failed to read temp image: %w", err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				return "", fmt.Errorf("failed to write image: %w", err)
			}
			os.Remove(tempPath)
		}
		fmt.Printf("✅ Image saved to %s\n", path)
		return path, nil
	}

	// Check if content is a Gemini file URI that needs downloading
	if strings.HasPrefix(content, "https://generativelanguage.googleapis.com/") && strings.Contains(content, "/files/") {
		if r.gemini != nil {
			fmt.Printf("📥 Downloading to %s...\n", path)
			_, err := r.gemini.DownloadFile(context.Background(), content, path)
			if err != nil {
				return "", fmt.Errorf("failed to download file: %w", err)
			}
			fmt.Printf("✅ Saved to %s\n", path)
			// Return just the path for chaining
			return path, nil
		}
		return "", fmt.Errorf("GEMINI_API_KEY required to download file")
	}

	// Check if content is a file path that exists (e.g., from TTS output)
	content = strings.TrimSpace(content)
	if _, err := os.Stat(content); err == nil {
		// Content is an existing file path - move/copy it to destination
		if err := os.Rename(content, path); err != nil {
			// If rename fails, try copy
			data, err := os.ReadFile(content)
			if err != nil {
				return "", fmt.Errorf("failed to read source file: %w", err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				return "", fmt.Errorf("failed to write file: %w", err)
			}
			os.Remove(content) // Clean up original
		}
		fmt.Printf("✅ Moved to %s\n", path)
		return path, nil
	}

	// Regular file save (text content)
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("✅ Saved %d bytes to %s\n", len(content), path)
	// Return just the path for chaining
	return path, nil
}

// read reads content from a file
func (r *Runtime) read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
}

// readStdin reads from standard input
func (r *Runtime) readStdin(prompt string) (string, error) {
	if prompt != "" {
		fmt.Printf("%s: ", prompt)
	} else {
		fmt.Print("Enter text (Ctrl+D to end): ")
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read stdin: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// list lists files in a directory
func (r *Runtime) list(path string) (string, error) {
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to list directory: %w", err)
	}

	var lines []string
	for _, entry := range entries {
		prefix := "📄"
		if entry.IsDir() {
			prefix = "📁"
		}
		lines = append(lines, fmt.Sprintf("%s %s", prefix, entry.Name()))
	}

	return strings.Join(lines, "\n"), nil
}

// email sends an email via Gmail API
func (r *Runtime) email(ctx context.Context, to string, content string) (string, error) {
	r.log("EMAIL to: %s", to)

	// Use Gemini to format as a beautiful HTML email
	prompt := fmt.Sprintf(`Format this content as a professional HTML email.
Return ONLY a JSON object with "subject" and "html" fields.

The HTML should:
- Have a nice header with "AgentScript" branding (use inline CSS, blue gradient background #667eea to #764ba2)
- Make all URLs clickable with proper <a href> tags styled as blue buttons
- Use clean, modern styling with good spacing
- Include a subtle footer
- Be mobile-responsive

Content to include:
%s

Return ONLY valid JSON like: {"subject": "...", "html": "<html>...</html>"}`, content)

	var subject, htmlBody string

	if r.gemini != nil {
		formatted, err := r.geminiCall(ctx, prompt)
		if err == nil {
			// Clean up response
			formatted = strings.TrimSpace(formatted)
			formatted = strings.TrimPrefix(formatted, "```json")
			formatted = strings.TrimPrefix(formatted, "```")
			formatted = strings.TrimSuffix(formatted, "```")
			formatted = strings.TrimSpace(formatted)

			var emailData struct {
				Subject string `json:"subject"`
				HTML    string `json:"html"`
				Body    string `json:"body"` // fallback
			}
			if json.Unmarshal([]byte(formatted), &emailData) == nil && emailData.Subject != "" {
				subject = emailData.Subject
				if emailData.HTML != "" {
					htmlBody = emailData.HTML
				} else if emailData.Body != "" {
					// Wrap plain body in basic HTML
					htmlBody = wrapInHTMLEmail(emailData.Body)
				}
			}
		}
	}

	if subject == "" {
		subject = "AgentScript Report"
		htmlBody = wrapInHTMLEmail(content)
	}

	// If Google client is available, send real email
	if r.google != nil {
		// Check if content contains a file path (for attachments)
		var attachmentPath string
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasSuffix(line, ".png") || strings.HasSuffix(line, ".jpg") ||
				strings.HasSuffix(line, ".jpeg") || strings.HasSuffix(line, ".pdf") ||
				strings.HasSuffix(line, ".mp4") || strings.HasSuffix(line, ".wav") ||
				strings.HasSuffix(line, ".mp3") {
				// Check if file exists
				if _, err := os.Stat(line); err == nil {
					attachmentPath = line
					break
				}
			}
		}

		if attachmentPath != "" {
			// Send with attachment
			err := r.google.SendEmailWithAttachment(ctx, to, subject, htmlBody, attachmentPath)
			if err != nil {
				return "", fmt.Errorf("failed to send email: %w", err)
			}
			return fmt.Sprintf("✅ Email sent to %s with attachment: %s", to, filepath.Base(attachmentPath)), nil
		}

		err := r.google.SendHTMLEmail(ctx, to, subject, htmlBody)
		if err != nil {
			return "", fmt.Errorf("failed to send email: %w", err)
		}
		return fmt.Sprintf("✅ Email sent to %s", to), nil
	}

	// Fallback: simulate sending
	fmt.Printf("\n📧 ========== EMAIL TO: %s ==========\n", to)
	fmt.Printf("Subject: %s\n\n[HTML Email]\n", subject)
	fmt.Println("📧 ======================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real email)")

	return fmt.Sprintf("Email simulated to %s", to), nil
}

// wrapInHTMLEmail wraps plain text in a nice HTML template
func wrapInHTMLEmail(content string) string {
	// Convert URLs to clickable button-style links
	urlRegex := regexp.MustCompile(`(https?://[^\s<>"]+)`)
	content = urlRegex.ReplaceAllString(content, `<a href="$1" style="display:inline-block;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);color:#fff;padding:10px 20px;border-radius:5px;text-decoration:none;margin:5px 0;">$1</a>`)

	// Convert newlines to breaks
	content = strings.ReplaceAll(content, "\n", "<br>\n")

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen,Ubuntu,sans-serif;background-color:#f5f5f5;">
  <div style="max-width:600px;margin:0 auto;background:#fff;">
    <div style="background:linear-gradient(135deg,#667eea 0%%,#764ba2 100%%);padding:30px;text-align:center;">
      <h1 style="color:#fff;margin:0;font-size:28px;">🚀 AgentScript</h1>
      <p style="color:rgba(255,255,255,0.8);margin:5px 0 0 0;font-size:14px;">AI-Powered Automation</p>
    </div>
    <div style="padding:30px;line-height:1.8;color:#333;">
      %s
    </div>
    <div style="background:#f9f9f9;padding:20px;text-align:center;font-size:12px;color:#888;border-top:1px solid #eee;">
      <p style="margin:0;">Sent via <strong>AgentScript</strong> • Powered by Gemini</p>
    </div>
  </div>
</body>
</html>`, content)
}

// calendar creates a Google Calendar event
func (r *Runtime) calendar(ctx context.Context, eventInfo string, content string) (string, error) {
	r.log("CALENDAR: %s", eventInfo)

	// Combine eventInfo and content for parsing
	fullText := eventInfo
	if content != "" {
		fullText = content
		if eventInfo != "" {
			fullText = eventInfo + "\n" + content
		}
	}

	// Get user's timezone if available
	timezone := "America/Los_Angeles" // default
	if r.google != nil {
		timezone = r.google.GetTimezone()
	}

	// Use Gemini to parse event details - support MULTIPLE events
	today := time.Now().Format("2006-01-02")
	prompt := fmt.Sprintf(`Today is %s. The user's timezone is %s.

Parse this text and extract ALL events/meetings/tasks with times.

Return ONLY a valid JSON array (no markdown, no explanation) where each object has:
- summary: event title (string)
- description: event description (string) 
- start: start time in RFC3339 format with timezone offset (e.g., 2026-02-10T15:00:00-08:00 for PST)
- end: end time in RFC3339 format (if not specified, assume 1 hour after start)

Important:
- If the date says "tomorrow", calculate the actual date from today.
- If it says "Monday", find the next Monday from today.
- If only time is given without date, assume today.
- Use the user's timezone (%s) for all times.
- "around 2ish" means 2:00 PM, "after lunch" means 1:00 PM, "before noon" means 11:00 AM, "end of day" means 5:00 PM

Text to parse:
%s

Return ONLY the JSON array like [{"summary":"...", "description":"...", "start":"...", "end":"..."}, ...]. Even if there's only one event, return it as an array.`, today, timezone, timezone, fullText)

	type EventData struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Start       string `json:"start"`
		End         string `json:"end"`
	}

	var events []EventData

	if r.gemini != nil {
		parsed, err := r.geminiCall(ctx, prompt)
		if err == nil {
			// Clean up the response - remove markdown code blocks if present
			parsed = strings.TrimSpace(parsed)
			parsed = strings.TrimPrefix(parsed, "```json")
			parsed = strings.TrimPrefix(parsed, "```")
			parsed = strings.TrimSuffix(parsed, "```")
			parsed = strings.TrimSpace(parsed)

			if err := json.Unmarshal([]byte(parsed), &events); err != nil {
				// Try single event fallback
				var single EventData
				if json.Unmarshal([]byte(parsed), &single) == nil && single.Summary != "" {
					events = []EventData{single}
				} else {
					r.log("Failed to parse calendar JSON: %v\nResponse: %s", err, parsed)
				}
			}
		}
	}

	// Fallback if no events parsed
	if len(events) == 0 {
		now := time.Now()
		events = []EventData{{
			Summary:     eventInfo,
			Description: content,
			Start:       now.Add(1 * time.Hour).Format(time.RFC3339),
			End:         now.Add(2 * time.Hour).Format(time.RFC3339),
		}}
	}

	// Create all events
	var results []string

	if r.google != nil {
		fmt.Printf("Creating %d calendar event(s)...\n", len(events))
		for i, ev := range events {
			event, err := r.google.CreateCalendarEvent(ctx, ev.Summary, ev.Description, ev.Start, ev.End)
			if err != nil {
				results = append(results, fmt.Sprintf("%d. FAILED: %s - %v", i+1, ev.Summary, err))
			} else {
				results = append(results, fmt.Sprintf("%d. %s\n   Time: %s\n   Link: %s", i+1, event.Summary, ev.Start, event.HtmlLink))
			}
		}
		return fmt.Sprintf("Created %d calendar event(s):\n%s", len(events), strings.Join(results, "\n")), nil
	}

	// Fallback: simulate
	fmt.Printf("\n========== CALENDAR EVENTS (%d) ==========\n", len(events))
	for i, ev := range events {
		fmt.Printf("%d. %s\n", i+1, ev.Summary)
		fmt.Printf("   Start: %s\n", ev.Start)
		fmt.Printf("   End: %s\n", ev.End)
		fmt.Printf("   Description: %s\n", ev.Description)
	}
	fmt.Println("==========================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real calendar)")

	return fmt.Sprintf("Calendar events simulated: %d events", len(events)), nil
}

// meet creates a Google Meet event
func (r *Runtime) meet(ctx context.Context, eventInfo string, content string) (string, error) {
	r.log("MEET: %s", eventInfo)

	// Use Gemini to parse event details
	prompt := fmt.Sprintf(`Parse this meeting information and return ONLY a JSON object with these fields:
- summary: meeting title
- description: meeting description/agenda
- start: start time in RFC3339 format (e.g., 2024-01-15T10:00:00-08:00)
- end: end time in RFC3339 format

Meeting info: %s

Additional context:
%s`, eventInfo, content)

	var summary, description, startTime, endTime string

	if r.gemini != nil {
		parsed, err := r.geminiCall(ctx, prompt)
		if err == nil {
			var eventData struct {
				Summary     string `json:"summary"`
				Description string `json:"description"`
				Start       string `json:"start"`
				End         string `json:"end"`
			}
			if json.Unmarshal([]byte(parsed), &eventData) == nil {
				summary = eventData.Summary
				description = eventData.Description
				startTime = eventData.Start
				endTime = eventData.End
			}
		}
	}

	if summary == "" {
		summary = eventInfo
		description = content
		now := time.Now()
		startTime = now.Add(1 * time.Hour).Format(time.RFC3339)
		endTime = now.Add(2 * time.Hour).Format(time.RFC3339)
	}

	// If Google client is available, create real Meet event
	if r.google != nil {
		event, err := r.google.CreateMeetEvent(ctx, summary, description, startTime, endTime)
		if err != nil {
			return "", fmt.Errorf("failed to create Meet event: %w", err)
		}
		meetLink := ""
		if event.ConferenceData != nil && len(event.ConferenceData.EntryPoints) > 0 {
			meetLink = event.ConferenceData.EntryPoints[0].Uri
		}
		return fmt.Sprintf("✅ Meet created: %s\nMeet Link: %s\nCalendar: %s", event.Summary, meetLink, event.HtmlLink), nil
	}

	// Fallback: simulate
	fmt.Printf("\n📹 ========== GOOGLE MEET ==========\n")
	fmt.Printf("Summary: %s\n", summary)
	fmt.Printf("Start: %s\n", startTime)
	fmt.Printf("Meet Link: (would be generated)\n")
	fmt.Println("📹 ====================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Meet)")

	return fmt.Sprintf("Meet simulated: %s", summary), nil
}

// driveSave saves content to Google Drive
func (r *Runtime) driveSave(ctx context.Context, path string, content string) (string, error) {
	r.log("DRIVE_SAVE: %s", path)

	if r.google != nil {
		file, err := r.google.SaveToDrive(ctx, path, content)
		if err != nil {
			return "", fmt.Errorf("failed to save to Drive: %w", err)
		}
		return fmt.Sprintf("✅ Saved to Google Drive: %s\nFile ID: %s", file.Name, file.Id), nil
	}

	// Fallback: simulate
	fmt.Printf("\n📁 ========== GOOGLE DRIVE ==========\n")
	fmt.Printf("Path: %s\n", path)
	fmt.Printf("Content: %d bytes\n", len(content))
	fmt.Println("📁 ====================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Drive)")

	return fmt.Sprintf("Drive save simulated: %s", path), nil
}

// docCreate creates a Google Doc
func (r *Runtime) docCreate(ctx context.Context, title string, content string) (string, error) {
	r.log("DOC_CREATE: %s", title)

	if r.google != nil {
		doc, err := r.google.CreateDoc(ctx, title, content)
		if err != nil {
			return "", fmt.Errorf("failed to create Doc: %w", err)
		}
		return fmt.Sprintf("✅ Google Doc created: %s\nLink: https://docs.google.com/document/d/%s", doc.Title, doc.DocumentId), nil
	}

	// Fallback: simulate
	fmt.Printf("\n📄 ========== GOOGLE DOC ==========\n")
	fmt.Printf("Title: %s\n", title)
	fmt.Printf("Content: %d bytes\n", len(content))
	fmt.Println("📄 ==================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Docs)")

	return fmt.Sprintf("Doc create simulated: %s", title), nil
}

// sheetAppend appends data to a Google Sheet
func (r *Runtime) sheetAppend(ctx context.Context, sheetRef string, content string) (string, error) {
	r.log("SHEET_APPEND: %s", sheetRef)

	// Parse sheetRef: "spreadsheetId/SheetName" or just "spreadsheetId"
	parts := strings.SplitN(sheetRef, "/", 2)
	spreadsheetID := parts[0]
	sheetName := ""
	if len(parts) > 1 {
		sheetName = parts[1]
	}

	if r.google != nil {
		err := r.google.AppendToSheet(ctx, spreadsheetID, sheetName, content)
		if err != nil {
			return "", fmt.Errorf("failed to append to Sheet: %w", err)
		}
		return fmt.Sprintf("✅ Data appended to Google Sheet: %s", spreadsheetID), nil
	}

	// Fallback: simulate
	fmt.Printf("\n📊 ========== GOOGLE SHEET ==========\n")
	fmt.Printf("Spreadsheet: %s\n", spreadsheetID)
	fmt.Printf("Sheet: %s\n", sheetName)
	fmt.Printf("Data: %d bytes\n", len(content))
	fmt.Println("📊 ====================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Sheets)")

	return fmt.Sprintf("Sheet append simulated: %s", sheetRef), nil
}

// sheetCreate creates a new Google Sheet
func (r *Runtime) sheetCreate(ctx context.Context, title string, content string) (string, error) {
	r.log("SHEET_CREATE: %s", title)

	if r.google != nil {
		sheet, err := r.google.CreateSheet(ctx, title)
		if err != nil {
			return "", fmt.Errorf("failed to create Sheet: %w", err)
		}

		// If there's content, append it
		if content != "" {
			err = r.google.AppendToSheet(ctx, sheet.SpreadsheetId, "", content)
			if err != nil {
				return "", fmt.Errorf("failed to add data to Sheet: %w", err)
			}
		}

		return fmt.Sprintf("✅ Google Sheet created: %s\nLink: %s", sheet.Properties.Title, sheet.SpreadsheetUrl), nil
	}

	// Fallback: simulate
	fmt.Printf("\n📊 ========== CREATE GOOGLE SHEET ==========\n")
	fmt.Printf("Title: %s\n", title)
	fmt.Printf("Initial data: %d bytes\n", len(content))
	fmt.Println("📊 ==========================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Sheets)")

	return fmt.Sprintf("Sheet create simulated: %s", title), nil
}

// task creates a Google Task
func (r *Runtime) task(ctx context.Context, title string, notes string) (string, error) {
	r.log("TASK: %s", title)

	if r.google != nil {
		task, err := r.google.CreateTask(ctx, title, notes)
		if err != nil {
			return "", fmt.Errorf("failed to create Task: %w", err)
		}
		return fmt.Sprintf("✅ Task created: %s", task.Title), nil
	}

	// Fallback: simulate
	fmt.Printf("\n✓ ========== GOOGLE TASK ==========\n")
	fmt.Printf("Title: %s\n", title)
	fmt.Printf("Notes: %s\n", notes)
	fmt.Println("✓ ==================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Tasks)")

	return fmt.Sprintf("Task simulated: %s", title), nil
}

// contactFind finds a contact by name
func (r *Runtime) contactFind(ctx context.Context, name string) (string, error) {
	r.log("CONTACT_FIND: %s", name)

	if r.google != nil {
		contacts, err := r.google.FindContact(ctx, name)
		if err != nil {
			return "", fmt.Errorf("failed to find contact: %w", err)
		}

		if len(contacts) == 0 {
			return fmt.Sprintf("No contacts found for: %s", name), nil
		}

		var results []string
		for _, contact := range contacts {
			var contactName, email string
			if len(contact.Names) > 0 {
				contactName = contact.Names[0].DisplayName
			}
			if len(contact.EmailAddresses) > 0 {
				email = contact.EmailAddresses[0].Value
			}
			results = append(results, fmt.Sprintf("%s <%s>", contactName, email))
		}
		return strings.Join(results, "\n"), nil
	}

	// Fallback: simulate
	fmt.Printf("\n👤 ========== GOOGLE CONTACTS ==========\n")
	fmt.Printf("Searching for: %s\n", name)
	fmt.Println("👤 ======================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real Contacts)")

	return fmt.Sprintf("Contact search simulated: %s", name), nil
}

// youtubeSearch searches YouTube
func (r *Runtime) youtubeSearch(ctx context.Context, query string) (string, error) {
	r.log("YOUTUBE_SEARCH: %s", query)

	if r.google != nil {
		results, err := r.google.SearchYouTube(ctx, query, 5)
		if err != nil {
			return "", fmt.Errorf("failed to search YouTube: %w", err)
		}

		if len(results) == 0 {
			return fmt.Sprintf("No videos found for: %s", query), nil
		}

		var videos []string
		for _, item := range results {
			videoURL := fmt.Sprintf("https://youtube.com/watch?v=%s", item.Id.VideoId)
			videos = append(videos, fmt.Sprintf("📺 %s\n   %s\n   %s", item.Snippet.Title, item.Snippet.Description[:min(100, len(item.Snippet.Description))], videoURL))
		}
		return strings.Join(videos, "\n\n"), nil
	}

	// Fallback: simulate
	fmt.Printf("\n📺 ========== YOUTUBE SEARCH ==========\n")
	fmt.Printf("Query: %s\n", query)
	fmt.Println("📺 =====================================")
	fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real YouTube)")

	return fmt.Sprintf("YouTube search simulated: %s", query), nil
}

// imageGenerate generates an image using Gemini Imagen
func (r *Runtime) imageGenerate(ctx context.Context, prompt string, input string) (string, error) {
	r.log("IMAGE_GENERATE: %s", prompt)

	// Combine prompt with any input context
	fullPrompt := prompt
	if input != "" {
		fullPrompt = prompt + ". Context: " + input
	}

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for image generation")
	}

	imageBytes, err := r.gemini.GenerateImage(ctx, fullPrompt)
	if err != nil {
		return "", fmt.Errorf("image generation failed: %w", err)
	}

	// Store the image bytes in a temp file and return a special marker
	// that save() can detect and handle properly
	tempFile := fmt.Sprintf(".temp_image_%d.png", time.Now().UnixNano())
	if err := os.WriteFile(tempFile, imageBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to save temp image: %w", err)
	}

	fmt.Printf("✅ Image generated (%d bytes)\n", len(imageBytes))

	// Return the temp file path - save command will move it to the final location
	return "IMAGEFILE:" + tempFile, nil
}

// imageAnalyze analyzes an image file
func (r *Runtime) imageAnalyze(ctx context.Context, imagePath string, prompt string) (string, error) {
	r.log("IMAGE_ANALYZE: %s", imagePath)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for image analysis")
	}

	// Default prompt if none provided
	analysisPrompt := prompt
	if analysisPrompt == "" {
		analysisPrompt = "Describe this image in detail. What do you see?"
	}

	result, err := r.gemini.AnalyzeImage(ctx, imagePath, analysisPrompt)
	if err != nil {
		return "", fmt.Errorf("image analysis failed: %w", err)
	}

	return result, nil
}

// videoAnalyze analyzes a video file
func (r *Runtime) videoAnalyze(ctx context.Context, videoPath string, prompt string) (string, error) {
	r.log("VIDEO_ANALYZE: %s", videoPath)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for video analysis")
	}

	// Default prompt if none provided
	analysisPrompt := prompt
	if analysisPrompt == "" {
		analysisPrompt = "Describe what happens in this video. Summarize the key moments and content."
	}

	result, err := r.gemini.AnalyzeVideo(ctx, videoPath, analysisPrompt)
	if err != nil {
		return "", fmt.Errorf("video analysis failed: %w", err)
	}

	return result, nil
}

// videoGenerate generates a video from a text prompt
func (r *Runtime) videoGenerate(ctx context.Context, prompt string, input string) (string, error) {
	r.log("VIDEO_GENERATE: %s", prompt)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for video generation")
	}

	// Combine prompt with input context if available
	fullPrompt := prompt
	if input != "" {
		fullPrompt = prompt + ". Additional context: " + input
	}

	// Check if vertical/shorts video is requested
	isVertical := strings.Contains(strings.ToLower(prompt), "vertical") ||
		strings.Contains(strings.ToLower(prompt), "shorts") ||
		strings.Contains(strings.ToLower(prompt), "9:16") ||
		strings.Contains(strings.ToLower(prompt), "portrait")

	if isVertical {
		fmt.Println("🎬 Generating vertical video for Shorts (this may take a few minutes)...")
	} else {
		fmt.Println("🎬 Generating video (this may take a few minutes)...")
	}

	videoURI, err := r.gemini.GenerateVideo(ctx, fullPrompt, isVertical)
	if err != nil {
		return "", fmt.Errorf("video generation failed: %w", err)
	}

	fmt.Printf("✅ Video generated!\n")

	// Return the URI - can be piped to save command
	return videoURI, nil
}

// imagesToVideo generates a video from multiple images
func (r *Runtime) imagesToVideo(ctx context.Context, imagesArg string, input string) (string, error) {
	r.log("IMAGES_TO_VIDEO: arg=%s, input=%s", imagesArg, input)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for video generation")
	}

	// Parse image paths from both the argument and piped input
	var imagePaths []string

	// Helper to extract image paths from text
	extractPaths := func(text string) []string {
		var paths []string
		// Replace newlines and commas with spaces, then split
		normalized := strings.ReplaceAll(text, "\n", " ")
		normalized = strings.ReplaceAll(normalized, ",", " ")
		normalized = strings.ReplaceAll(normalized, "===", " ")
		normalized = strings.ReplaceAll(normalized, "Branch", " ")
		for _, p := range strings.Fields(normalized) {
			path := strings.TrimSpace(p)
			// Check for image extensions
			lower := strings.ToLower(path)
			if strings.HasSuffix(lower, ".jpg") ||
				strings.HasSuffix(lower, ".jpeg") ||
				strings.HasSuffix(lower, ".png") ||
				strings.HasSuffix(lower, ".webp") ||
				strings.HasSuffix(lower, ".gif") {
				paths = append(paths, path)
			}
		}
		return paths
	}

	// First try to get paths from the argument
	if imagesArg != "" {
		imagePaths = append(imagePaths, extractPaths(imagesArg)...)
	}

	// Also extract from piped input (merged parallel output)
	if input != "" {
		imagePaths = append(imagePaths, extractPaths(input)...)
	}

	// Deduplicate paths while preserving order
	seen := make(map[string]bool)
	var uniquePaths []string
	for _, p := range imagePaths {
		if !seen[p] {
			seen[p] = true
			uniquePaths = append(uniquePaths, p)
		}
	}
	imagePaths = uniquePaths

	if len(imagePaths) == 0 {
		return "", fmt.Errorf("no image paths found. Use: images_to_video \"img1.png img2.png\" or pipe from parallel image generation")
	}

	// Use a default video prompt, or use piped input if it looks like a description
	videoPrompt := "Create a smooth cinematic video transitioning between these images"
	if input != "" {
		// Check if input contains a description (not just file paths)
		words := strings.Fields(input)
		descWords := 0
		for _, word := range words {
			lower := strings.ToLower(word)
			if !strings.HasSuffix(lower, ".png") &&
				!strings.HasSuffix(lower, ".jpg") &&
				!strings.Contains(lower, "===") &&
				!strings.Contains(lower, "branch") &&
				len(word) > 2 {
				descWords++
			}
		}
		// If there's substantial text beyond file paths, might be a description
		if descWords > 5 {
			// Extract potential description
			for _, keyword := range []string{"smooth", "transition", "cinematic", "pan", "zoom", "animate"} {
				if strings.Contains(strings.ToLower(input), keyword) {
					videoPrompt = input
					break
				}
			}
		}
	}

	fmt.Printf("🎬 Generating video from %d images (this may take a few minutes)...\n", len(imagePaths))
	for i, p := range imagePaths {
		fmt.Printf("   %d. %s\n", i+1, p)
	}
	fmt.Printf("   Prompt: %s\n", videoPrompt)

	videoURI, err := r.gemini.GenerateVideoFromImages(ctx, imagePaths, videoPrompt)
	if err != nil {
		return "", fmt.Errorf("video generation failed: %w", err)
	}

	fmt.Printf("✅ Video generated from %d images!\n", len(imagePaths))

	// Return URI so it can be piped to save
	return videoURI, nil
}

// textToSpeech converts text to speech using Gemini TTS
func (r *Runtime) textToSpeech(ctx context.Context, voice string, input string) (string, error) {
	r.log("TEXT_TO_SPEECH: voice=%s, input=%d bytes", voice, len(input))

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for text-to-speech")
	}

	// Default voice if not specified
	if voice == "" {
		voice = "Kore"
	}

	// Use the piped input as the text to speak
	text := input
	if text == "" {
		return "", fmt.Errorf("no text to convert to speech - pipe text into text_to_speech")
	}

	fmt.Printf("🎙️ Converting text to speech (voice: %s)...\n", voice)

	audioPath, err := r.gemini.TextToSpeech(ctx, text, voice)
	if err != nil {
		return "", fmt.Errorf("text-to-speech failed: %w", err)
	}

	fmt.Printf("✅ Audio generated: %s\n", audioPath)
	return audioPath, nil
}

// audioVideoMerge combines an audio file with a video file using ffmpeg
func (r *Runtime) audioVideoMerge(ctx context.Context, outputName string, input string) (string, error) {
	r.log("AUDIO_VIDEO_MERGE: output=%s, input=%s", outputName, input)

	// Parse input - expect "audio.wav video.mp4" or merged parallel output
	var audioPath, videoPath string

	// Try to extract paths from input
	parts := strings.Fields(input)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "===" || part == "Branch" {
			continue
		}
		if strings.HasSuffix(part, ".wav") || strings.HasSuffix(part, ".mp3") || strings.HasSuffix(part, ".m4a") {
			audioPath = part
		} else if strings.HasSuffix(part, ".mp4") || strings.HasSuffix(part, ".mov") || strings.HasSuffix(part, ".webm") {
			videoPath = part
		}
	}

	if audioPath == "" {
		return "", fmt.Errorf("no audio file found in input - need .wav, .mp3, or .m4a file")
	}
	if videoPath == "" {
		return "", fmt.Errorf("no video file found in input - need .mp4, .mov, or .webm file")
	}

	// Check files exist
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		return "", fmt.Errorf("audio file not found: %s", audioPath)
	}
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		return "", fmt.Errorf("video file not found: %s", videoPath)
	}

	// Default output name
	if outputName == "" {
		outputName = "merged_output.mp4"
	}
	if !strings.HasSuffix(outputName, ".mp4") {
		outputName = outputName + ".mp4"
	}

	fmt.Printf("🎬 Merging audio and video with ffmpeg...\n")
	fmt.Printf("   Audio: %s\n", audioPath)
	fmt.Printf("   Video: %s\n", videoPath)

	// Use ffmpeg to merge - replace video audio with our audio
	// -shortest makes output length match the shorter of the two inputs
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "aac",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-shortest",
		outputName,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if ffmpeg is installed
		if strings.Contains(err.Error(), "executable file not found") {
			fmt.Printf("\n❌ ffmpeg not found.\n")
			fmt.Printf("\n📋 Install ffmpeg:\n")
			fmt.Printf("   macOS:  brew install ffmpeg\n")
			fmt.Printf("   Ubuntu: sudo apt install ffmpeg\n")
			fmt.Printf("   Windows: choco install ffmpeg\n\n")
			return "", fmt.Errorf("ffmpeg required for audio_video_merge - please install it")
		}
		return "", fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("✅ Merged video saved: %s\n", outputName)
	return outputName, nil
}

// imageAudioMerge creates a video from a static image and audio using ffmpeg
// This is a fallback when Veo quota is exhausted
func (r *Runtime) imageAudioMerge(ctx context.Context, outputName string, input string) (string, error) {
	r.log("IMAGE_AUDIO_MERGE: output=%s, input=%s", outputName, input)

	// Parse input - expect "image.png" and "audio.wav" paths
	var imagePath, audioPath string

	parts := strings.Fields(input)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "===" || part == "Branch" {
			continue
		}
		if strings.HasSuffix(part, ".wav") || strings.HasSuffix(part, ".mp3") || strings.HasSuffix(part, ".m4a") {
			audioPath = part
		} else if strings.HasSuffix(part, ".png") || strings.HasSuffix(part, ".jpg") || strings.HasSuffix(part, ".jpeg") {
			imagePath = part
		}
	}

	if imagePath == "" {
		return "", fmt.Errorf("no image file found in input - need .png or .jpg file")
	}
	if audioPath == "" {
		return "", fmt.Errorf("no audio file found in input - need .wav, .mp3, or .m4a file")
	}

	// Check files exist
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("image file not found: %s", imagePath)
	}
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		return "", fmt.Errorf("audio file not found: %s", audioPath)
	}

	// Default output name
	if outputName == "" {
		outputName = "image_video.mp4"
	}
	if !strings.HasSuffix(outputName, ".mp4") {
		outputName = outputName + ".mp4"
	}

	fmt.Printf("🎬 Creating video from image + audio with ffmpeg...\n")
	fmt.Printf("   Image: %s\n", imagePath)
	fmt.Printf("   Audio: %s\n", audioPath)

	// Use ffmpeg to create video from static image + audio
	// -loop 1: loop the image
	// -shortest: end when audio ends
	// -c:v libx264: H.264 video codec
	// -tune stillimage: optimize for still image
	// -c:a aac: AAC audio codec
	// -pix_fmt yuv420p: compatible pixel format
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-loop", "1",
		"-i", imagePath,
		"-i", audioPath,
		"-c:v", "libx264",
		"-tune", "stillimage",
		"-c:a", "aac",
		"-b:a", "192k",
		"-pix_fmt", "yuv420p",
		"-shortest",
		outputName,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			fmt.Printf("\n❌ ffmpeg not found.\n")
			fmt.Printf("\n📋 Install ffmpeg:\n")
			fmt.Printf("   macOS:  brew install ffmpeg\n")
			fmt.Printf("   Ubuntu: sudo apt install ffmpeg\n")
			fmt.Printf("   Windows: choco install ffmpeg\n\n")
			return "", fmt.Errorf("ffmpeg required for image_audio_merge - please install it")
		}
		return "", fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("✅ Video created: %s\n", outputName)
	return outputName, nil
}

// mapsTrip creates a Google Maps trip URL from a list of places
func (r *Runtime) mapsTrip(ctx context.Context, tripName string, input string) (string, error) {
	r.log("MAPS_TRIP: %s", tripName)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for maps_trip")
	}

	// Use Gemini to extract places and coordinates
	prompt := fmt.Sprintf(`From this text, extract all place names that can be visited.
Return ONLY a JSON array of objects with "name" and "address" fields.
The address should be specific enough for Google Maps (include city/country).

Example output:
[{"name": "Dubrovnik Old Town", "address": "Dubrovnik, Croatia"}, {"name": "Kotor", "address": "Kotor, Montenegro"}]

Text:
%s

Return ONLY the JSON array, nothing else.`, input)

	parsed, err := r.geminiCall(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to parse places: %w", err)
	}

	// Clean up response
	parsed = strings.TrimSpace(parsed)
	parsed = strings.TrimPrefix(parsed, "```json")
	parsed = strings.TrimPrefix(parsed, "```")
	parsed = strings.TrimSuffix(parsed, "```")
	parsed = strings.TrimSpace(parsed)

	var places []struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}

	if err := json.Unmarshal([]byte(parsed), &places); err != nil {
		return "", fmt.Errorf("failed to parse places JSON: %w", err)
	}

	if len(places) == 0 {
		return "", fmt.Errorf("no places found in input")
	}

	// Build Google Maps directions URL
	// Format: https://www.google.com/maps/dir/place1/place2/place3/...
	var encodedPlaces []string
	for _, p := range places {
		// URL encode the address
		encoded := strings.ReplaceAll(p.Address, " ", "+")
		encodedPlaces = append(encodedPlaces, encoded)
	}

	mapsURL := "https://www.google.com/maps/dir/" + strings.Join(encodedPlaces, "/")

	fmt.Printf("🗺️ Created trip with %d stops:\n", len(places))
	for i, p := range places {
		fmt.Printf("   %d. %s\n", i+1, p.Name)
	}
	fmt.Printf("\n📍 Google Maps Trip URL:\n%s\n\n", mapsURL)

	// Return formatted result
	result := fmt.Sprintf("Trip: %s\n\nStops:\n", tripName)
	for i, p := range places {
		result += fmt.Sprintf("%d. %s - %s\n", i+1, p.Name, p.Address)
	}
	result += fmt.Sprintf("\nGoogle Maps Route:\n%s", mapsURL)

	return result, nil
}

// formCreate creates a Google Form using LLM to generate questions
func (r *Runtime) formCreate(ctx context.Context, formTitle string, input string) (string, error) {
	r.log("FORM_CREATE: %s", formTitle)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for form_create")
	}

	// Use Gemini to generate form questions from input
	prompt := fmt.Sprintf(`Based on this context, create a survey/form with appropriate questions.

Return ONLY a valid JSON object with this structure:
{
  "title": "Form Title",
  "description": "Brief description of the form",
  "questions": [
    {"title": "Question text", "type": "text", "required": true},
    {"title": "Multiple choice question", "type": "multiple_choice", "required": true, "options": ["Option 1", "Option 2", "Option 3"]},
    {"title": "Checkbox question", "type": "checkbox", "required": false, "options": ["Choice A", "Choice B"]}
  ]
}

Question types: text, paragraph, multiple_choice, checkbox, dropdown

Context: %s
Form title hint: %s

Return ONLY the JSON, no explanation.`, input, formTitle)

	parsed, err := r.geminiCall(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate form questions: %w", err)
	}

	// Clean up response
	parsed = strings.TrimSpace(parsed)
	parsed = strings.TrimPrefix(parsed, "```json")
	parsed = strings.TrimPrefix(parsed, "```")
	parsed = strings.TrimSuffix(parsed, "```")
	parsed = strings.TrimSpace(parsed)

	var formData struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Questions   []struct {
			Title    string   `json:"title"`
			Type     string   `json:"type"`
			Required bool     `json:"required"`
			Options  []string `json:"options"`
		} `json:"questions"`
	}

	if err := json.Unmarshal([]byte(parsed), &formData); err != nil {
		return "", fmt.Errorf("failed to parse form JSON: %w\nResponse: %s", err, parsed)
	}

	if formTitle != "" {
		formData.Title = formTitle
	}

	if r.google == nil {
		// Simulate if no Google client
		fmt.Printf("\n📋 ========== FORM (Simulated) ==========\n")
		fmt.Printf("Title: %s\n", formData.Title)
		fmt.Printf("Description: %s\n", formData.Description)
		fmt.Printf("Questions:\n")
		for i, q := range formData.Questions {
			fmt.Printf("  %d. %s (%s)\n", i+1, q.Title, q.Type)
			if len(q.Options) > 0 {
				fmt.Printf("     Options: %v\n", q.Options)
			}
		}
		fmt.Println("📋 =========================================")
		fmt.Println("(Simulated - set GOOGLE_CREDENTIALS_FILE for real form)")
		return fmt.Sprintf("Form simulated: %s with %d questions", formData.Title, len(formData.Questions)), nil
	}

	// Convert to FormQuestion slice
	var questions []FormQuestion
	for _, q := range formData.Questions {
		questions = append(questions, FormQuestion{
			Title:    q.Title,
			Type:     q.Type,
			Required: q.Required,
			Options:  q.Options,
		})
	}

	// Create real form
	formURL, editURL, err := r.google.CreateForm(ctx, formData.Title, formData.Description, questions)
	if err != nil {
		return "", fmt.Errorf("failed to create form: %w", err)
	}

	fmt.Printf("✅ Form created: %s\n", formData.Title)
	fmt.Printf("📋 Fill out: %s\n", formURL)
	fmt.Printf("✏️ Edit: %s\n", editURL)

	result := fmt.Sprintf("Form created: %s\n\nShare this link to collect responses:\n%s\n\nEdit form:\n%s", formData.Title, formURL, editURL)
	return result, nil
}

// formResponses retrieves responses from a Google Form
func (r *Runtime) formResponses(ctx context.Context, formId string, input string) (string, error) {
	r.log("FORM_RESPONSES: %s", formId)

	// If formId not provided as arg, try to extract from input
	if formId == "" {
		// Try to find form ID in input (from previous form_create output)
		if strings.Contains(input, "forms/d/") {
			parts := strings.Split(input, "forms/d/")
			if len(parts) > 1 {
				formId = strings.Split(parts[1], "/")[0]
			}
		}
	}

	if formId == "" {
		return "", fmt.Errorf("form ID required - provide as argument or pipe from form_create")
	}

	if r.google == nil {
		return "", fmt.Errorf("GOOGLE_CREDENTIALS_FILE required for form_responses")
	}

	responses, err := r.google.GetFormResponses(ctx, formId)
	if err != nil {
		return "", fmt.Errorf("failed to get responses: %w", err)
	}

	if len(responses) == 0 {
		return "No responses yet.", nil
	}

	fmt.Printf("📊 Found %d responses\n", len(responses))

	// Format responses
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Form Responses (%d total):\n\n", len(responses)))

	for i, resp := range responses {
		result.WriteString(fmt.Sprintf("--- Response %d ---\n", i+1))
		if email, ok := resp["respondent"].(string); ok && email != "" {
			result.WriteString(fmt.Sprintf("From: %s\n", email))
		}
		if submitted, ok := resp["submitted"].(string); ok && submitted != "" {
			result.WriteString(fmt.Sprintf("Submitted: %s\n", submitted))
		}
		if answers, ok := resp["answers"].(map[string]string); ok {
			for question, answer := range answers {
				result.WriteString(fmt.Sprintf("Q: %s\nA: %s\n", question, answer))
			}
		}
		result.WriteString("\n")
	}

	return result.String(), nil
}

// translate translates text to a target language using Gemini
func (r *Runtime) translate(ctx context.Context, targetLang string, input string) (string, error) {
	r.log("TRANSLATE: to %s, input=%d bytes", targetLang, len(input))

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for translate")
	}

	if input == "" {
		return "", fmt.Errorf("no text to translate - pipe text into translate")
	}

	if targetLang == "" {
		targetLang = "Spanish"
	}

	prompt := fmt.Sprintf("Translate the following text to %s. Return ONLY the translated text, nothing else:\n\n%s", targetLang, input)

	fmt.Printf("🌐 Translating to %s...\n", targetLang)

	translated, err := r.geminiCall(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("translation failed: %w", err)
	}

	fmt.Printf("✅ Translation complete\n")
	return translated, nil
}

// placesSearch searches for places using Google Places API (via Gemini for now)
func (r *Runtime) placesSearch(ctx context.Context, query string, input string) (string, error) {
	r.log("PLACES_SEARCH: %s", query)

	if r.gemini == nil {
		return "", fmt.Errorf("GEMINI_API_KEY required for places_search")
	}

	// Combine query and input
	searchQuery := query
	if input != "" && query == "" {
		searchQuery = input
	} else if input != "" {
		searchQuery = query + " near " + input
	}

	if searchQuery == "" {
		return "", fmt.Errorf("no search query - provide query as argument or pipe location")
	}

	prompt := fmt.Sprintf(`Search for places: %s

Return a list of 10 places with the following information for each:
- Name
- Address  
- Rating (out of 5)
- Brief description
- Google Maps link (format: https://www.google.com/maps/search/PLACE+NAME+ADDRESS)

Format as a numbered list.`, searchQuery)

	fmt.Printf("📍 Searching for places: %s...\n", searchQuery)

	result, err := r.geminiCall(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("places search failed: %w", err)
	}

	fmt.Printf("✅ Found places\n")
	return result, nil
}

// mcpConnect connects to an MCP server
func (r *Runtime) mcpConnect(ctx context.Context, serverName string, command string) (string, error) {
	r.log("MCP_CONNECT: name=%s, command=%s", serverName, command)

	if serverName == "" {
		return "", fmt.Errorf("server name required: mcp_connect \"name\" \"command\"")
	}

	// Command can come from arg or input
	cmd := command
	if cmd == "" {
		return "", fmt.Errorf("command required: mcp_connect \"name\" \"npx -y @modelcontextprotocol/server-xxx\"")
	}

	// Check for required tokens and prompt for OAuth if missing
	if strings.Contains(cmd, "server-github") && os.Getenv("GITHUB_TOKEN") == "" && os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN") == "" {
		fmt.Println("🔑 GitHub token not found.")

		// Check for saved token first
		tokenFile := "github_mcp_token.txt"
		if data, err := os.ReadFile(tokenFile); err == nil && len(data) > 0 {
			token := strings.TrimSpace(string(data))
			os.Setenv("GITHUB_TOKEN", token)
			os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", token)
			fmt.Println("✅ Loaded GitHub token from", tokenFile)
		} else {
			// Open browser to create token
			fmt.Println("\n📋 Opening browser to create a GitHub Personal Access Token...")
			fmt.Println("   1. Sign in to GitHub")
			fmt.Println("   2. Create a token with 'repo' scope")
			fmt.Println("   3. Copy the token\n")

			tokenURL := "https://github.com/settings/tokens/new?description=AgentScript%20MCP&scopes=repo,read:user"
			exec.Command("open", tokenURL).Start()     // macOS
			exec.Command("xdg-open", tokenURL).Start() // Linux

			fmt.Print("Paste your GitHub token here: ")
			var token string
			fmt.Scanln(&token)
			token = strings.TrimSpace(token)

			if token == "" {
				return "", fmt.Errorf("no token provided")
			}

			// Save token for future use
			os.WriteFile(tokenFile, []byte(token), 0600)
			os.Setenv("GITHUB_TOKEN", token)
			os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", token)
			fmt.Println("✅ Token saved to", tokenFile)
		}
	} else if strings.Contains(cmd, "server-github") {
		// Ensure both env vars are set
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", token)
		} else if token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); token != "" {
			os.Setenv("GITHUB_TOKEN", token)
		}
	}

	if strings.Contains(cmd, "server-slack") && os.Getenv("SLACK_TOKEN") == "" {
		return "", fmt.Errorf("SLACK_TOKEN not set. Get a token from https://api.slack.com/apps")
	}

	if strings.Contains(cmd, "server-brave") && os.Getenv("BRAVE_API_KEY") == "" {
		return "", fmt.Errorf("BRAVE_API_KEY not set. Get a key from https://brave.com/search/api/")
	}

	fmt.Printf("🔌 Connecting to MCP server '%s'...\n", serverName)
	fmt.Printf("   Command: %s\n", cmd)

	if err := r.mcp.Connect(ctx, serverName, cmd); err != nil {
		return "", fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	// List available tools
	tools, err := r.mcp.ListTools(serverName)
	if err != nil {
		return "", fmt.Errorf("failed to list tools: %w", err)
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Connected to MCP server '%s'\n", serverName))
	result.WriteString(fmt.Sprintf("Available tools (%d):\n", len(tools)))
	for _, tool := range tools {
		result.WriteString(fmt.Sprintf("  - %s: %s\n", tool.Name, tool.Description))
	}

	fmt.Printf("✅ Connected to '%s' with %d tools\n", serverName, len(tools))
	return result.String(), nil
}

// mcpList lists available tools from a connected MCP server
func (r *Runtime) mcpList(ctx context.Context, serverName string, input string) (string, error) {
	r.log("MCP_LIST: server=%s", serverName)

	// If no server specified, list all connected servers
	if serverName == "" {
		servers := r.mcp.ListServers()
		if len(servers) == 0 {
			return "No MCP servers connected. Use mcp_connect first.", nil
		}
		var result strings.Builder
		result.WriteString("Connected MCP servers:\n")
		for _, s := range servers {
			tools, _ := r.mcp.ListTools(s)
			result.WriteString(fmt.Sprintf("  - %s (%d tools)\n", s, len(tools)))
		}
		return result.String(), nil
	}

	tools, err := r.mcp.ListTools(serverName)
	if err != nil {
		return "", err
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Tools for '%s':\n", serverName))
	for _, tool := range tools {
		result.WriteString(fmt.Sprintf("\n%s\n", tool.Name))
		result.WriteString(fmt.Sprintf("  Description: %s\n", tool.Description))
		if tool.InputSchema != nil {
			if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
				result.WriteString("  Parameters:\n")
				for name, schema := range props {
					if s, ok := schema.(map[string]interface{}); ok {
						result.WriteString(fmt.Sprintf("    - %s: %v\n", name, s["description"]))
					}
				}
			}
		}
	}
	return result.String(), nil
}

// mcpCall calls a tool on an MCP server
func (r *Runtime) mcpCall(ctx context.Context, arg string, argsJSON string) (string, error) {
	r.log("MCP_CALL: arg=%s, argsJSON=%s", arg, argsJSON)

	// Parse arg: "server:tool"
	parts := strings.SplitN(arg, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid mcp call format. Use: mcp \"server:tool\" '{\"arg\": \"value\"}'")
	}

	serverName := strings.TrimSpace(parts[0])
	toolName := strings.TrimSpace(parts[1])

	fmt.Printf("🔧 Calling %s.%s...\n", serverName, toolName)

	result, err := r.mcp.CallTool(ctx, serverName, toolName, argsJSON)
	if err != nil {
		return "", fmt.Errorf("MCP call failed: %w", err)
	}

	fmt.Printf("✅ MCP call complete\n")
	return result, nil
}

// videoScript converts content into a Veo-optimized video prompt with synchronized dialogue
// This is the KEY command for creating narrated videos - Veo 3.1 will generate video WITH
// synchronized audio/speech, so we don't need separate TTS!
func (r *Runtime) videoScript(ctx context.Context, style string, input string) (string, error) {
	r.log("VIDEO_SCRIPT: style=%s, input=%d bytes", style, len(input))

	if r.gemini == nil && r.claude == nil {
		return "", fmt.Errorf("GEMINI_API_KEY or CLAUDE_API_KEY required for video script generation")
	}

	if input == "" {
		return "", fmt.Errorf("no content to convert - pipe content into video_script")
	}

	// Default style
	if style == "" {
		style = "news anchor"
	}

	fmt.Printf("📝 Converting to Veo video prompt (style: %s)...\n", style)
	fmt.Printf("   Veo 3.1 will generate synchronized audio automatically!\n")

	prompt := fmt.Sprintf(`Convert the following content into a video generation prompt for Veo 3.1 with SYNCHRONIZED DIALOGUE.

CONTENT TO CONVERT:
%s

STYLE: %s

CRITICAL REQUIREMENTS:
1. Veo 3.1 generates video WITH synchronized audio - the speaker's lips will move in sync with dialogue
2. Put ALL spoken words in quotes - Veo will generate speech for quoted text
3. Keep dialogue UNDER 20 words (must fit in 8 seconds at natural speaking pace)
4. Describe the visual scene, camera angle, lighting
5. Add SFX: for sound effects, Ambient: for background sounds
6. For vertical/shorts: include "portrait 9:16 aspect ratio"

OUTPUT FORMAT (return ONLY this prompt, no explanation):
[Visual scene description, camera angle, lighting]. [Speaker description] speaking directly to camera: "[DIALOGUE UNDER 20 WORDS]". SFX: [sound]. Ambient: [background].

EXAMPLE:
Modern news studio with blue accent lighting, medium close-up shot, portrait 9:16 aspect ratio. Professional female news anchor in business attire speaking directly to camera: "Breaking tonight - tech giants announce major layoffs as AI reshapes the workforce." SFX: subtle news intro tone. Ambient: quiet studio atmosphere.

NOW CONVERT THE CONTENT ABOVE:`, input, style)

	var result string
	var err error

	if r.claude != nil {
		result, err = r.claude.Chat(ctx, prompt)
	} else {
		result, err = r.geminiCall(ctx, prompt)
	}

	if err != nil {
		return "", fmt.Errorf("video script generation failed: %w", err)
	}

	result = strings.TrimSpace(result)

	// Validate it has quoted dialogue
	if !strings.Contains(result, "\"") {
		fmt.Printf("⚠️  Warning: No quoted dialogue found - Veo may not generate speech\n")
	}

	fmt.Printf("✅ Video prompt ready - Veo will sync lips to dialogue!\n")

	return result, nil
}

// youtubeUpload uploads a video to YouTube (or YouTube Shorts)
func (r *Runtime) youtubeUpload(ctx context.Context, title string, input string, isShorts bool) (string, error) {
	r.log("YOUTUBE_UPLOAD: title=%s, input=%s, shorts=%v", title, input, isShorts)

	if r.google == nil {
		return "", fmt.Errorf("GOOGLE_CREDENTIALS_FILE required for YouTube upload")
	}

	// Input should be a video file path
	videoPath := strings.TrimSpace(input)
	if videoPath == "" {
		return "", fmt.Errorf("no video file path provided - pipe a video path into youtube_upload")
	}

	// Check if file exists
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		return "", fmt.Errorf("video file not found: %s", videoPath)
	}

	// For Shorts, add #Shorts to title if not present
	finalTitle := title
	description := "Uploaded via AgentScript"
	if isShorts {
		if !strings.Contains(title, "#Shorts") && !strings.Contains(title, "#shorts") {
			finalTitle = title + " #Shorts"
		}
		description = "Uploaded via AgentScript #Shorts"
		fmt.Printf("📱 Uploading to YouTube Shorts: %s...\n", finalTitle)
	} else {
		fmt.Printf("📺 Uploading to YouTube: %s...\n", finalTitle)
	}

	videoURL, err := r.google.UploadToYouTube(ctx, videoPath, finalTitle, description)
	if err != nil {
		return "", fmt.Errorf("YouTube upload failed: %w", err)
	}

	fmt.Printf("✅ Video uploaded: %s\n", videoURL)
	return videoURL, nil
}

// confirm prompts user for confirmation before continuing
func (r *Runtime) confirm(ctx context.Context, message string, input string) (string, error) {
	r.log("CONFIRM: %s", message)

	// Display what we're confirming
	if message == "" {
		message = "Continue with this action?"
	}

	fmt.Printf("\n⚠️  CONFIRMATION REQUIRED\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("%s\n", message)
	if input != "" {
		// Show truncated input if it's a file path or short
		if len(input) < 200 {
			fmt.Printf("Input: %s\n", input)
		} else {
			fmt.Printf("Input: %s... (%d bytes)\n", input[:100], len(input))
		}
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Proceed? [y/N]: ")

	var response string
	fmt.Scanln(&response)

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "y" || response == "yes" {
		fmt.Printf("✅ Confirmed. Continuing...\n")
		return input, nil // Pass through the input unchanged
	}

	return "", fmt.Errorf("operation cancelled by user")
}

// githubPages deploys content as a React SPA to GitHub Pages
func (r *Runtime) githubPages(ctx context.Context, title string, input string) (string, error) {
	r.log("GITHUB_PAGES: title=%s, input=%d bytes", title, len(input))

	if r.github == nil {
		fmt.Printf("\n❌ GitHub API not configured.\n")
		fmt.Printf("\n📋 Setup GitHub OAuth:\n")
		fmt.Printf("   1. Go to: https://github.com/settings/developers\n")
		fmt.Printf("   2. Click 'New OAuth App'\n")
		fmt.Printf("   3. Fill in app name, homepage URL, callback URL (http://localhost)\n")
		fmt.Printf("   4. Copy Client ID and Client Secret\n")
		fmt.Printf("\n💡 Add to your .env file:\n")
		fmt.Printf("   GITHUB_CLIENT_ID=your-client-id\n")
		fmt.Printf("   GITHUB_CLIENT_SECRET=your-client-secret\n\n")
		return "", fmt.Errorf("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET required")
	}

	if input == "" {
		return "", fmt.Errorf("no content to deploy - pipe content into github_pages")
	}

	if title == "" {
		title = "AgentScript Page"
	}

	// Create repo name from title
	repoName := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	repoName = strings.ReplaceAll(repoName, "'", "")
	repoName = strings.ReplaceAll(repoName, "\"", "")

	// Use Claude if available, otherwise fall back to Gemini
	var reactCode string
	var err error

	if r.claude != nil {
		fmt.Printf("🎨 Generating React SPA with Claude...\n")
		reactCode, err = r.claude.GenerateReactSPA(ctx, title, input)
		if err != nil {
			return "", fmt.Errorf("Claude React generation failed: %w", err)
		}
	} else if r.gemini != nil {
		fmt.Printf("🎨 Generating React SPA with Gemini...\n")
		reactCode, err = r.generateReactSPA(ctx, title, input)
		if err != nil {
			return "", fmt.Errorf("Gemini React generation failed: %w", err)
		}
	} else {
		fmt.Printf("\n❌ No AI API key configured for React SPA generation.\n")
		fmt.Printf("\n📋 Get your API keys:\n")
		fmt.Printf("   Claude (recommended): https://console.anthropic.com/settings/keys\n")
		fmt.Printf("   Gemini:               https://aistudio.google.com/apikey\n")
		fmt.Printf("\n💡 Then add to your .env file:\n")
		fmt.Printf("   CLAUDE_API_KEY=sk-ant-...\n")
		fmt.Printf("   GEMINI_API_KEY=...\n\n")
		return "", fmt.Errorf("CLAUDE_API_KEY or GEMINI_API_KEY required for github_pages")
	}

	fmt.Printf("🚀 Deploying to GitHub Pages: %s...\n", title)

	pagesURL, err := r.github.DeployReactSPA(ctx, repoName, title, reactCode)
	if err != nil {
		return "", fmt.Errorf("GitHub Pages deployment failed: %w", err)
	}

	fmt.Printf("✅ Deployed to: %s\n", pagesURL)
	fmt.Printf("   (Note: May take 1-2 minutes to go live)\n")

	return pagesURL, nil
}

// generateReactSPA uses Gemini to create a React single-page application
func (r *Runtime) generateReactSPA(ctx context.Context, title string, content string) (string, error) {
	prompt := fmt.Sprintf(`Generate a beautiful, modern React single-page application (SPA) for the following content.

TITLE: %s

CONTENT:
%s

REQUIREMENTS:
1. Output ONLY the complete HTML file with embedded React (using babel standalone)
2. Use React hooks (useState, useEffect)
3. Modern, dark theme UI with gradients and animations
4. Responsive design with Tailwind CSS (via CDN)
5. Include smooth scroll animations
6. Add a navigation header if content has sections
7. Use React icons or emojis for visual appeal
8. Make it visually stunning - this is for a hackathon demo!
9. Include a footer crediting "Built with AgentScript"

OUTPUT FORMAT:
Return ONLY the HTML code starting with <!DOCTYPE html> and ending with </html>
No markdown, no explanation, just the raw HTML/React code.`, title, content)

	result, err := r.geminiCall(ctx, prompt)
	if err != nil {
		return "", err
	}

	// Clean up the response - remove any markdown code blocks if present
	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```html")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	// Validate it looks like HTML
	if !strings.HasPrefix(result, "<!DOCTYPE html>") && !strings.HasPrefix(result, "<html") {
		// Try to find HTML in the response
		if idx := strings.Index(result, "<!DOCTYPE html>"); idx != -1 {
			result = result[idx:]
		} else if idx := strings.Index(result, "<html"); idx != -1 {
			result = result[idx:]
		} else {
			return "", fmt.Errorf("Gemini did not return valid HTML")
		}
	}

	return result, nil
}

// githubPagesHTML deploys content as simple HTML to GitHub Pages (no AI generation)
func (r *Runtime) githubPagesHTML(ctx context.Context, title string, input string) (string, error) {
	r.log("GITHUB_PAGES_HTML: title=%s, input=%d bytes", title, len(input))

	if r.github == nil {
		fmt.Printf("\n❌ GitHub API not configured.\n")
		fmt.Printf("\n📋 Setup GitHub OAuth:\n")
		fmt.Printf("   1. Go to: https://github.com/settings/developers\n")
		fmt.Printf("   2. Click 'New OAuth App'\n")
		fmt.Printf("   3. Fill in app name, homepage URL, callback URL (http://localhost)\n")
		fmt.Printf("   4. Copy Client ID and Client Secret\n")
		fmt.Printf("\n💡 Add to your .env file:\n")
		fmt.Printf("   GITHUB_CLIENT_ID=your-client-id\n")
		fmt.Printf("   GITHUB_CLIENT_SECRET=your-client-secret\n\n")
		return "", fmt.Errorf("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET required")
	}

	if input == "" {
		return "", fmt.Errorf("no content to deploy - pipe content into github_pages_html")
	}

	if title == "" {
		title = "AgentScript Page"
	}

	// Create repo name from title
	repoName := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	repoName = strings.ReplaceAll(repoName, "'", "")
	repoName = strings.ReplaceAll(repoName, "\"", "")

	fmt.Printf("🚀 Deploying simple HTML to GitHub Pages: %s...\n", title)

	pagesURL, err := r.github.DeployToPages(ctx, repoName, title, input)
	if err != nil {
		return "", fmt.Errorf("GitHub Pages deployment failed: %w", err)
	}

	fmt.Printf("✅ Deployed to: %s\n", pagesURL)
	fmt.Printf("   (Note: May take 1-2 minutes to go live)\n")

	return pagesURL, nil
}

// ============================================================================
// Data Command Handlers
// ============================================================================

func (r *Runtime) jobSearchCmd(ctx context.Context, query, location, empType, input string) (string, error) {
	if query == "" && input != "" {
		query = strings.TrimSpace(input)
	}
	if query == "" {
		return "", fmt.Errorf("job_search requires a query")
	}
	searcher := NewJobSearcher(r.searchKey, r.verbose)
	config := JobSearchConfig{Query: query, Location: location, EmploymentType: empType, NumPages: 1}
	return CachedGet(r.cache, "jobs", query+location, CacheTTLJobs, func() (string, error) {
		jobs, err := searcher.Search(ctx, config)
		if err != nil {
			return "", err
		}
		return FormatJobResults(jobs), nil
	})
}

func (r *Runtime) weatherFetch(ctx context.Context, location, input string) (string, error) {
	if location == "" {
		location = strings.TrimSpace(input)
	}
	if location == "" {
		return "", fmt.Errorf("weather requires a location")
	}
	client := NewWeatherClient(r.verbose)
	return CachedGet(r.cache, "weather", location, CacheTTLWeather, func() (string, error) {
		data, err := client.GetWeather(ctx, location)
		if err != nil {
			return "", err
		}
		return FormatWeather(data), nil
	})
}

func (r *Runtime) newsFetch(ctx context.Context, query, input string) (string, error) {
	if query == "" {
		query = strings.TrimSpace(input)
	}
	if query == "" {
		return "", fmt.Errorf("news requires a query")
	}
	client := NewNewsClient(os.Getenv("GNEWS_API_KEY"), r.searchKey, r.verbose)
	return CachedGet(r.cache, "news", query, CacheTTLNews, func() (string, error) {
		articles, err := client.Search(ctx, query, 10)
		if err != nil {
			return "", err
		}
		return FormatNewsResults(articles, query), nil
	})
}

func (r *Runtime) newsHeadlines(ctx context.Context, category, input string) (string, error) {
	if category == "" {
		category = "general"
	}
	client := NewNewsClient(os.Getenv("GNEWS_API_KEY"), r.searchKey, r.verbose)
	return CachedGet(r.cache, "headlines", category, CacheTTLNews, func() (string, error) {
		articles, err := client.TopHeadlines(ctx, category, 10)
		if err != nil {
			return "", err
		}
		return FormatNewsResults(articles, category+" headlines"), nil
	})
}

func (r *Runtime) stockFetch(ctx context.Context, symbols, input string) (string, error) {
	if symbols == "" {
		symbols = strings.TrimSpace(input)
	}
	if symbols == "" {
		return "", fmt.Errorf("stock requires symbol(s)")
	}
	client := NewStockClient(os.Getenv("FINNHUB_API_KEY"), r.searchKey, r.verbose)
	return CachedGet(r.cache, "stock", symbols, CacheTTLStock, func() (string, error) {
		parsed := ParseStockSymbols(symbols)
		var quotes []StockQuote
		for _, sym := range parsed {
			q, err := client.GetQuote(ctx, sym)
			if err != nil {
				return "", err
			}
			quotes = append(quotes, *q)
		}
		return FormatStockQuotes(quotes), nil
	})
}

func (r *Runtime) cryptoFetch(ctx context.Context, arg, input string) (string, error) {
	query := arg
	if query == "" {
		query = strings.TrimSpace(input)
	}
	if query == "" {
		query = "BTC,ETH,SOL"
	}
	return CachedGet(r.cache, "crypto", query, CacheTTLCrypto, func() (string, error) {
		symbols := ParseCryptoSymbols(query)
		if symbols == nil {
			n := 10
			fmt.Sscanf(strings.ToLower(query), "top %d", &n)
			prices, err := r.crypto.GetTopN(ctx, n)
			if err != nil {
				return "", err
			}
			return FormatCryptoPrices(prices), nil
		}
		prices, err := r.crypto.GetPrices(ctx, symbols)
		if err != nil {
			return "", err
		}
		return FormatCryptoPrices(prices), nil
	})
}

func (r *Runtime) redditFetch(ctx context.Context, arg, arg2, input string) (string, error) {
	query := arg
	if query == "" {
		query = strings.TrimSpace(input)
	}
	if query == "" {
		return "", fmt.Errorf("reddit requires a subreddit or search query")
	}
	args := []string{query}
	if arg2 != "" {
		args = append(args, arg2)
	}
	isSub, q, sort := ParseRedditArgs(args...)
	return CachedGet(r.cache, "reddit", q+sort, CacheTTLReddit, func() (string, error) {
		var posts []RedditPost
		var err error
		if isSub {
			posts, err = r.reddit.SearchSubreddit(ctx, q, sort, 10)
		} else {
			posts, err = r.reddit.SearchReddit(ctx, q, sort, 10)
		}
		if err != nil {
			return "", err
		}
		return FormatRedditPosts(posts, q), nil
	})
}

func (r *Runtime) rssFetch(ctx context.Context, feedURL, input string) (string, error) {
	u := feedURL
	if u == "" {
		u = strings.TrimSpace(input)
	}
	if u == "" {
		return ListFeedShortcuts(), nil
	}
	return CachedGet(r.cache, "rss", u, CacheTTLRSS, func() (string, error) {
		items, title, err := r.rss.FetchFeed(ctx, u, 10)
		if err != nil {
			return "", err
		}
		return FormatRSSItems(items, title), nil
	})
}

func (r *Runtime) twitterFetch(ctx context.Context, query, input string) (string, error) {
	if query == "" {
		query = strings.TrimSpace(input)
	}
	if query == "" {
		return "", fmt.Errorf("twitter requires a search query")
	}
	tweets, err := r.twitter.SearchRecent(ctx, query, 10)
	if err != nil {
		return "", err
	}
	return FormatTweets(tweets, query), nil
}

// ============================================================================
// Notification Handlers
// ============================================================================

func (r *Runtime) notifyCmd(ctx context.Context, target, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("notify needs piped content")
	}
	return r.notifier.Send(ctx, input, target)
}

func (r *Runtime) whatsappSend(ctx context.Context, to, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("whatsapp needs piped content")
	}
	return r.whatsapp.Send(ctx, to, input)
}

// ============================================================================
// Control Flow Handlers
// ============================================================================

func (r *Runtime) foreachCmd(ctx context.Context, separator, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("foreach needs piped input")
	}
	config := ParseForEachArgs(separator)
	items := ParseLoopItems(input, config.Separator)
	results, err := ExecuteForEach(ctx, items, config, r.verbose,
		func(ctx context.Context, item string, index int) (string, error) {
			return item, nil
		},
	)
	if err != nil {
		return "", err
	}
	return FormatForEachResults(results), nil
}

func (r *Runtime) ifCmd(ctx context.Context, condition, input string) (string, error) {
	result, err := EvaluateConditionString(condition, input, r.vars)
	if err != nil {
		return "", fmt.Errorf("if condition error: %w", err)
	}
	r.log("IF %q -> %v", condition, result)
	if result {
		return input, nil
	}
	return "", nil
}

// ============================================================================
// Hugging Face Handlers
// ============================================================================

func (r *Runtime) hfGenerate(ctx context.Context, prompt, model, input string) (string, error) {
	if prompt == "" {
		prompt = input
	}
	return r.hf.TextGeneration(ctx, prompt, model, 500)
}

func (r *Runtime) hfSummarize(ctx context.Context, model, input string) (string, error) {
	text := input
	if text == "" {
		text = model
		model = ""
	}
	return r.hf.Summarize(ctx, text, model)
}

func (r *Runtime) hfClassify(ctx context.Context, text, model, input string) (string, error) {
	if text == "" {
		text = input
	}
	results, err := r.hf.Classify(ctx, text, model)
	if err != nil {
		return "", err
	}
	return FormatClassification(results, text), nil
}

func (r *Runtime) hfNER(ctx context.Context, text, model, input string) (string, error) {
	if text == "" {
		text = input
	}
	results, err := r.hf.NER(ctx, text, model)
	if err != nil {
		return "", err
	}
	return FormatNER(results, text), nil
}

func (r *Runtime) hfTranslateCmd(ctx context.Context, text, srcLang, tgtLang, input string) (string, error) {
	if text == "" {
		text = input
	}
	if srcLang == "" {
		srcLang = "en"
	}
	if tgtLang == "" {
		tgtLang = "fr"
	}
	return r.hf.Translate(ctx, text, srcLang, tgtLang)
}

func (r *Runtime) hfEmbeddings(ctx context.Context, text, model, input string) (string, error) {
	if text == "" {
		text = input
	}
	texts := strings.Split(text, "\n")
	embeddings, err := r.hf.Embeddings(ctx, texts, model)
	if err != nil {
		return "", err
	}
	if len(embeddings) > 0 {
		return fmt.Sprintf("Generated %d embeddings of dimension %d", len(embeddings), len(embeddings[0])), nil
	}
	return "No embeddings generated", nil
}

func (r *Runtime) hfQA(ctx context.Context, question, contextArg, input string) (string, error) {
	contextText := contextArg
	if contextText == "" {
		contextText = input
	}
	result, err := r.hf.QuestionAnswer(ctx, question, contextText, "")
	if err != nil {
		return "", err
	}
	return FormatQA(result, question), nil
}

func (r *Runtime) hfFillMask(ctx context.Context, text, model, input string) (string, error) {
	if text == "" {
		text = input
	}
	results, err := r.hf.FillMask(ctx, text, model)
	if err != nil {
		return "", err
	}
	return FormatFillMask(results), nil
}

func (r *Runtime) hfZeroShot(ctx context.Context, text, labelsStr, input string) (string, error) {
	if text == "" {
		text = input
	}
	labels := []string{"positive", "negative", "neutral"}
	if labelsStr != "" {
		labels = strings.Split(labelsStr, ",")
		for i := range labels {
			labels[i] = strings.TrimSpace(labels[i])
		}
	}
	result, err := r.hf.ZeroShotClassify(ctx, text, labels, "")
	if err != nil {
		return "", err
	}
	return FormatZeroShot(result), nil
}

func (r *Runtime) hfImageGen(ctx context.Context, prompt, model, input string) (string, error) {
	if prompt == "" {
		prompt = input
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

func (r *Runtime) hfImageClassify(ctx context.Context, imagePath, input string) (string, error) {
	if imagePath == "" {
		imagePath = strings.TrimSpace(input)
	}
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image %s: %w", imagePath, err)
	}
	body, err := r.hf.callInferenceRaw(ctx, resolveModel("image_classify", ""), data, "image/jpeg")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (r *Runtime) hfSpeechToText(ctx context.Context, audioPath, input string) (string, error) {
	if audioPath == "" {
		audioPath = strings.TrimSpace(input)
	}
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio %s: %w", audioPath, err)
	}
	body, err := r.hf.callInferenceRaw(ctx, resolveModel("asr", ""), data, "audio/flac")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (r *Runtime) hfSimilarityCmd(ctx context.Context, source, sentencesStr, input string) (string, error) {
	if source == "" {
		source = input
	}
	var sentences []string
	if sentencesStr != "" {
		sentences = strings.Split(sentencesStr, ",")
		for i := range sentences {
			sentences[i] = strings.TrimSpace(sentences[i])
		}
	}
	scores, err := r.hf.SentenceSimilarity(ctx, source, sentences, "")
	if err != nil {
		return "", err
	}
	return FormatSimilarity(source, sentences, scores), nil
}

// ============================================================================
// Emoji Style Handler
// ============================================================================

func (r *Runtime) emojiStyleCmd(ctx context.Context, emojisArg, styleArg, engineArg, input string) (string, error) {
	// Auto-detect: arg1 might be emojis or style
	emojis := emojisArg
	style := styleArg
	engine := engineArg

	// If emojis arg looks like a style (no emoji chars), swap
	if emojis != "" && !isEmojiInput(emojis) && style == "" {
		// Check if input has emojis instead
		if input != "" && (isEmojiInput(input) || strings.Contains(input, ".png") || strings.Contains(input, ".jpg")) {
			style = emojis
			emojis = input
			input = ""
		}
	}

	// If no emojis arg, use input
	if emojis == "" {
		emojis = strings.TrimSpace(input)
	}

	if emojis == "" {
		return "", fmt.Errorf("emoji_style requires emojis or image paths. Usage: emoji_style \"😀😎🔥\" \"wearing suits\" \"hf\"")
	}
	if style == "" {
		return "", fmt.Errorf("emoji_style requires a style description. Usage: emoji_style \"😀😎🔥\" \"wearing suits with red ties\"")
	}

	// Parse sources
	sources, err := DetectEmojiSources(emojis)
	if err != nil {
		return "", err
	}

	// Build config
	config := ParseEmojiStyleArgs(emojis, style, engine)
	config.OutputDir = fmt.Sprintf("stickers_%s", sanitizeName(style)[:min(20, len(sanitizeName(style)))])

	// Generate sticker pack
	results, err := r.emoji.CreateStickerPack(ctx, sources, config)
	if err != nil {
		return "", err
	}

	return FormatEmojiResults(results, config), nil
}

// log prints verbose output if enabled
func (r *Runtime) log(format string, args ...any) {
	if r.verbose {
		fmt.Printf("[agentscript] "+format+"\n", args...)
	}
}
