package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// WhatsAppClient handles WhatsApp messaging via Twilio
type WhatsAppClient struct {
	accountSID string
	authToken  string
	fromNumber string // Twilio WhatsApp sandbox or approved number
	client     *http.Client
	verbose    bool
}

// NewWhatsAppClient creates a new WhatsApp client
func NewWhatsAppClient(verbose bool) *WhatsAppClient {
	return &WhatsAppClient{
		accountSID: os.Getenv("TWILIO_ACCOUNT_SID"),
		authToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
		fromNumber: os.Getenv("TWILIO_WHATSAPP_FROM"), // e.g., "whatsapp:+14155238886"
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (wc *WhatsAppClient) log(format string, args ...any) {
	if wc.verbose {
		fmt.Printf("[WHATSAPP] "+format+"\n", args...)
	}
}

// IsConfigured returns true if Twilio credentials are set
func (wc *WhatsAppClient) IsConfigured() bool {
	return wc.accountSID != "" && wc.authToken != "" && wc.fromNumber != ""
}

// Send sends a WhatsApp message via Twilio
func (wc *WhatsAppClient) Send(ctx context.Context, to string, message string) (string, error) {
	if !wc.IsConfigured() {
		return "", fmt.Errorf("WhatsApp not configured. Set: TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_WHATSAPP_FROM")
	}

	// Normalize the "to" number
	to = normalizeWhatsAppNumber(to)

	// Twilio API endpoint
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", wc.accountSID)

	// Truncate message (WhatsApp limit is ~4096 chars for text)
	if len(message) > 4000 {
		message = message[:4000] + "\n\n... (truncated)"
	}

	// Build form data
	data := url.Values{}
	data.Set("From", wc.fromNumber)
	data.Set("To", to)
	data.Set("Body", message)

	wc.log("Sending to %s (%d chars)", to, len(message))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(wc.accountSID, wc.authToken)

	resp, err := wc.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Twilio request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var twilioErr struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		}
		json.Unmarshal(body, &twilioErr)
		return "", fmt.Errorf("Twilio error (status %d, code %d): %s",
			resp.StatusCode, twilioErr.Code, twilioErr.Message)
	}

	var result struct {
		SID    string `json:"sid"`
		Status string `json:"status"`
	}
	json.Unmarshal(body, &result)

	wc.log("Sent! SID: %s, Status: %s", result.SID, result.Status)

	return fmt.Sprintf("WhatsApp sent to %s (status: %s)\n\n%s", to, result.Status, message), nil
}

// normalizeWhatsAppNumber ensures the number is in whatsapp:+XXXXXXXXXXX format
func normalizeWhatsAppNumber(number string) string {
	number = strings.TrimSpace(number)

	// Already in correct format
	if strings.HasPrefix(number, "whatsapp:") {
		return number
	}

	// Strip common prefixes
	number = strings.TrimPrefix(number, "wa:")
	number = strings.TrimPrefix(number, "whatsapp:")

	// Ensure + prefix
	if !strings.HasPrefix(number, "+") {
		// Assume US number if 10 digits
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, number)
		if len(digits) == 10 {
			number = "+1" + digits
		} else if len(digits) > 0 {
			number = "+" + digits
		}
	}

	return "whatsapp:" + number
}
