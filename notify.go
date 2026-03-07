package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// NotifyClient handles webhook notifications
type NotifyClient struct {
	slackURL     string // Slack incoming webhook URL
	discordURL   string // Discord webhook URL
	telegramBot  string // Telegram bot token
	telegramChat string // Telegram chat ID
	client       *http.Client
	verbose      bool
}

// NewNotifyClient creates a new notification client from env vars
func NewNotifyClient(verbose bool) *NotifyClient {
	return &NotifyClient{
		slackURL:     os.Getenv("SLACK_WEBHOOK_URL"),
		discordURL:   os.Getenv("DISCORD_WEBHOOK_URL"),
		telegramBot:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		telegramChat: os.Getenv("TELEGRAM_CHAT_ID"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		verbose: verbose,
	}
}

func (nc *NotifyClient) log(format string, args ...any) {
	if nc.verbose {
		fmt.Printf("[NOTIFY] "+format+"\n", args...)
	}
}

// HasAnyWebhook returns true if at least one webhook is configured
func (nc *NotifyClient) HasAnyWebhook() bool {
	return nc.slackURL != "" || nc.discordURL != "" || nc.telegramBot != ""
}

// Send sends a notification to all configured channels
func (nc *NotifyClient) Send(ctx context.Context, message string, target string) (string, error) {
	if !nc.HasAnyWebhook() {
		return "", fmt.Errorf("no webhook configured. Set one of: SLACK_WEBHOOK_URL, DISCORD_WEBHOOK_URL, TELEGRAM_BOT_TOKEN+TELEGRAM_CHAT_ID")
	}

	// Parse target to send to specific channel
	targetLower := strings.ToLower(strings.TrimSpace(target))

	var results []string
	var errors []string

	switch targetLower {
	case "slack":
		if nc.slackURL == "" {
			return "", fmt.Errorf("SLACK_WEBHOOK_URL not set")
		}
		if err := nc.sendSlack(ctx, message); err != nil {
			return "", err
		}
		results = append(results, "Slack ✓")

	case "discord":
		if nc.discordURL == "" {
			return "", fmt.Errorf("DISCORD_WEBHOOK_URL not set")
		}
		if err := nc.sendDiscord(ctx, message); err != nil {
			return "", err
		}
		results = append(results, "Discord ✓")

	case "telegram":
		if nc.telegramBot == "" {
			return "", fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
		}
		if err := nc.sendTelegram(ctx, message); err != nil {
			return "", err
		}
		results = append(results, "Telegram ✓")

	default:
		// Send to ALL configured channels
		if nc.slackURL != "" {
			if err := nc.sendSlack(ctx, message); err != nil {
				errors = append(errors, fmt.Sprintf("Slack: %v", err))
			} else {
				results = append(results, "Slack ✓")
			}
		}

		if nc.discordURL != "" {
			if err := nc.sendDiscord(ctx, message); err != nil {
				errors = append(errors, fmt.Sprintf("Discord: %v", err))
			} else {
				results = append(results, "Discord ✓")
			}
		}

		if nc.telegramBot != "" && nc.telegramChat != "" {
			if err := nc.sendTelegram(ctx, message); err != nil {
				errors = append(errors, fmt.Sprintf("Telegram: %v", err))
			} else {
				results = append(results, "Telegram ✓")
			}
		}
	}

	if len(results) == 0 && len(errors) > 0 {
		return "", fmt.Errorf("all notifications failed: %s", strings.Join(errors, "; "))
	}

	summary := fmt.Sprintf("Notified: %s", strings.Join(results, ", "))
	if len(errors) > 0 {
		summary += fmt.Sprintf(" (failed: %s)", strings.Join(errors, "; "))
	}

	return summary + "\n\n" + message, nil
}

// sendSlack sends a message via Slack incoming webhook
func (nc *NotifyClient) sendSlack(ctx context.Context, message string) error {
	nc.log("Sending to Slack")

	// Truncate for Slack (max ~40k chars per message)
	if len(message) > 3000 {
		message = message[:3000] + "\n\n... (truncated)"
	}

	// Try rich blocks first, fall back to plain text
	blocks := formatSlackBlocks(message)

	payload := map[string]any{
		"text":   message, // fallback for notifications
		"blocks": blocks,
	}

	err := nc.postJSON(ctx, nc.slackURL, payload)
	if err != nil && strings.Contains(err.Error(), "invalid_blocks") {
		// Blocks failed — retry with plain text only
		nc.log("Blocks failed, retrying with plain text")
		plainPayload := map[string]any{
			"text": message,
		}
		return nc.postJSON(ctx, nc.slackURL, plainPayload)
	}
	return err
}

// sendDiscord sends a message via Discord webhook
func (nc *NotifyClient) sendDiscord(ctx context.Context, message string) error {
	nc.log("Sending to Discord")

	// Discord max is 2000 chars per message
	if len(message) > 1900 {
		message = message[:1900] + "\n\n... (truncated)"
	}

	payload := map[string]any{
		"content":  message,
		"username": "AgentScript",
	}

	return nc.postJSON(ctx, nc.discordURL, payload)
}

// sendTelegram sends a message via Telegram Bot API
func (nc *NotifyClient) sendTelegram(ctx context.Context, message string) error {
	nc.log("Sending to Telegram (chat: %s)", nc.telegramChat)

	if nc.telegramChat == "" {
		return fmt.Errorf("TELEGRAM_CHAT_ID not set")
	}

	// Telegram max is 4096 chars
	if len(message) > 4000 {
		message = message[:4000] + "\n\n... (truncated)"
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", nc.telegramBot)

	payload := map[string]any{
		"chat_id":    nc.telegramChat,
		"text":       message,
		"parse_mode": "Markdown",
	}

	return nc.postJSON(ctx, apiURL, payload)
}

// postJSON sends a JSON POST request
func (nc *NotifyClient) postJSON(ctx context.Context, url string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := nc.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// formatSlackBlocks converts markdown-ish text into rich Slack Block Kit blocks
func formatSlackBlocks(message string) []map[string]any {
	var blocks []map[string]any

	lines := strings.Split(message, "\n")
	var currentSection strings.Builder
	isFirst := true

	flushSection := func() {
		text := strings.TrimSpace(currentSection.String())
		if text == "" {
			return
		}
		// Convert markdown bold **text** to Slack bold *text*
		text = convertMarkdownToSlack(text)

		// Slack section text max is 3000 chars
		if len(text) > 3000 {
			text = text[:3000]
		}
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": text,
			},
		})
		currentSection.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Headers → Slack header block or bold divider
		if strings.HasPrefix(trimmed, "# ") {
			flushSection()
			title := strings.TrimPrefix(trimmed, "# ")
			title = strings.TrimPrefix(title, "## ")
			if isFirst {
				blocks = append(blocks, map[string]any{
					"type": "header",
					"text": map[string]string{
						"type":  "plain_text",
						"text":  title,
						"emoji": "true",
					},
				})
				isFirst = false
			} else {
				blocks = append(blocks, map[string]any{
					"type": "divider",
				})
				currentSection.WriteString("*" + title + "*\n")
			}
			continue
		}

		if strings.HasPrefix(trimmed, "## ") {
			flushSection()
			title := strings.TrimPrefix(trimmed, "## ")
			blocks = append(blocks, map[string]any{
				"type": "divider",
			})
			currentSection.WriteString("*" + title + "*\n")
			continue
		}

		// Horizontal rules → dividers
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			flushSection()
			blocks = append(blocks, map[string]any{
				"type": "divider",
			})
			continue
		}

		// Table rows → format as code block for alignment
		if strings.Contains(trimmed, "|") && strings.Count(trimmed, "|") >= 2 {
			// Skip markdown table separator rows
			if strings.Trim(trimmed, "|-: ") == "" {
				continue
			}
			currentSection.WriteString(trimmed + "\n")
			continue
		}

		// Empty lines between paragraphs
		if trimmed == "" {
			if currentSection.Len() > 0 {
				currentSection.WriteString("\n")
			}
			continue
		}

		isFirst = false
		currentSection.WriteString(trimmed + "\n")
	}

	flushSection()

	// Add timestamp footer
	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []map[string]string{
			{
				"type": "mrkdwn",
				"text": "📡 _Sent by AgentScript_ • " + time.Now().Format("Jan 2, 3:04 PM"),
			},
		},
	})

	return blocks
}

// convertMarkdownToSlack converts common markdown to Slack mrkdwn
func convertMarkdownToSlack(text string) string {
	// **bold** → *bold*
	for strings.Contains(text, "**") {
		start := strings.Index(text, "**")
		rest := text[start+2:]
		end := strings.Index(rest, "**")
		if end == -1 {
			break
		}
		text = text[:start] + "*" + rest[:end] + "*" + rest[end+2:]
	}

	// Convert bullet points: - item or * item → • item
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			lines[i] = "  • " + strings.TrimPrefix(trimmed, "- ")
		} else if strings.HasPrefix(trimmed, "* ") && !strings.HasPrefix(trimmed, "**") {
			lines[i] = "  • " + strings.TrimPrefix(trimmed, "* ")
		}
	}

	return strings.Join(lines, "\n")
}
