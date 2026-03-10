package geminilive

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"google.golang.org/genai"
)

// LiveSession wraps a Gemini Live API bidirectional streaming session.
type LiveSession struct {
	session *genai.Session
	logger  *slog.Logger
	mu      sync.Mutex
	closed  bool

	OnAudio     func(audio []byte)
	OnText      func(text string)
	OnToolCall  func(id, name string, args map[string]any)
	OnInterrupt func()
	OnTurnDone  func()
}

// LiveConfig configures a Live API session.
type LiveConfig struct {
	SystemPrompt string
	VoiceName    string
	Tools        []*genai.Tool
}

// NewLiveSession connects to Gemini Live API.
func NewLiveSession(ctx context.Context, cfg LiveConfig, logger *slog.Logger) (*LiveSession, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}
	if logger == nil {
		logger = slog.Default()
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      apiKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{APIVersion: "v1beta"},
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}

	model := "gemini-2.0-flash-exp-image-generation"
	if m := os.Getenv("GEMINI_LIVE_MODEL"); m != "" {
		model = m
	}

	config := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
	}
	if cfg.VoiceName != "" {
		config.SpeechConfig = &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: cfg.VoiceName},
			},
		}
	}
	if cfg.SystemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: cfg.SystemPrompt}},
		}
	}
	if len(cfg.Tools) > 0 {
		config.Tools = cfg.Tools
	}

	session, err := client.Live.Connect(ctx, model, config)
	if err != nil {
		return nil, fmt.Errorf("connect live: %w", err)
	}

	return &LiveSession{session: session, logger: logger}, nil
}

// StartReceiving runs the receive loop in a goroutine.
func (ls *LiveSession) StartReceiving(ctx context.Context) {
	go ls.receiveLoop(ctx)
}

func (ls *LiveSession) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := ls.session.Receive()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			ls.logger.Error("live.receive.error", "error", err)
			return
		}
		if msg == nil {
			continue
		}

		if msg.ServerContent != nil {
			sc := msg.ServerContent
			if sc.Interrupted && ls.OnInterrupt != nil {
				ls.OnInterrupt()
			}
			if sc.TurnComplete && ls.OnTurnDone != nil {
				ls.OnTurnDone()
			}
			if sc.ModelTurn != nil {
				for _, part := range sc.ModelTurn.Parts {
					if part.InlineData != nil && ls.OnAudio != nil {
						ls.OnAudio(part.InlineData.Data)
					}
					if part.Text != "" && ls.OnText != nil {
						ls.OnText(part.Text)
					}
				}
			}
		}

		if msg.ToolCall != nil {
			for _, fc := range msg.ToolCall.FunctionCalls {
				if ls.OnToolCall != nil {
					args := make(map[string]any)
					if fc.Args != nil {
						args = fc.Args
					}
					ls.OnToolCall(fc.ID, fc.Name, args)
				}
			}
		}
	}
}

// SendAudio sends PCM16 audio to the live session.
func (ls *LiveSession) SendAudio(pcm16 []byte) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return fmt.Errorf("session closed")
	}
	return ls.session.SendRealtimeInput(genai.LiveSendRealtimeInputParameters{
		Media: &genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     pcm16,
		},
	})
}

// SendImage sends a JPEG frame to the live session via SendClientContent
// so the model can actually see the image (SendRealtimeInput doesn't work for vision).
func (ls *LiveSession) SendImage(jpegB64 string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return fmt.Errorf("session closed")
	}
	jpeg, err := base64.StdEncoding.DecodeString(jpegB64)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	return ls.session.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "image/jpeg", Data: jpeg}},
			},
		}},
	})
}

// SendText sends text to the live session.
func (ls *LiveSession) SendText(text string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return fmt.Errorf("session closed")
	}
	return ls.session.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{{
			Role:  genai.RoleUser,
			Parts: []*genai.Part{{Text: text}},
		}},
	})
}

// SendToolResponse sends a function call result back to Gemini.
func (ls *LiveSession) SendToolResponse(id string, result map[string]any) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return fmt.Errorf("session closed")
	}
	return ls.session.SendToolResponse(genai.LiveToolResponseInput{
		FunctionResponses: []*genai.FunctionResponse{{
			ID:       id,
			Response: result,
		}},
	})
}

// Close ends the live session.
func (ls *LiveSession) Close() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if !ls.closed {
		ls.closed = true
		ls.session.Close()
	}
}
