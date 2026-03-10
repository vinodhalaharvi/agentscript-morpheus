package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	gl "github.com/vinodhalaharvi/agentscript/pkg/geminilive"
	"golang.org/x/term"

	"github.com/vinodhalaharvi/agentscript/internal/agentscript"
	"google.golang.org/genai"
)

// ============================================================================
// Generic StateMachine
// ============================================================================

type Transition[S comparable] struct {
	From    S
	To      S
	OnEnter func()
}

type StateMachine[S comparable] struct {
	current  atomic.Int64
	states   []S
	stateIdx map[S]int64
	allowed  map[S]map[S]func()
	mu       sync.Mutex
	logger   *slog.Logger
}

func NewStateMachine[S comparable](initial S, logger *slog.Logger) *StateMachine[S] {
	sm := &StateMachine[S]{
		states:   []S{initial},
		stateIdx: map[S]int64{initial: 0},
		allowed:  map[S]map[S]func(){},
		logger:   logger,
	}
	sm.current.Store(0)
	return sm
}

func (sm *StateMachine[S]) Register(from, to S, onEnter func()) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, ok := sm.stateIdx[to]; !ok {
		sm.stateIdx[to] = int64(len(sm.states))
		sm.states = append(sm.states, to)
	}
	if _, ok := sm.allowed[from]; !ok {
		sm.allowed[from] = map[S]func(){}
	}
	sm.allowed[from][to] = onEnter
}

func (sm *StateMachine[S]) State() S {
	return sm.states[sm.current.Load()]
}

func (sm *StateMachine[S]) Transition(to S) bool {
	sm.mu.Lock()
	cur := sm.states[sm.current.Load()]
	toIdx, knownTo := sm.stateIdx[to]
	onEnter, allowed := sm.allowed[cur][to]
	sm.mu.Unlock()

	if !knownTo || !allowed {
		sm.logger.Debug("rejected state transition", "from", fmt.Sprint(cur), "to", fmt.Sprint(to))
		return false
	}
	sm.current.Store(toIdx)
	if onEnter != nil {
		onEnter()
	}
	return true
}

func (sm *StateMachine[S]) Is(s S) bool { return sm.State() == s }

// ============================================================================
// Generic Handler + Filter + Bus
// ============================================================================

type Handler[I, O any] func(ctx context.Context, input I) (O, error)

func Filter[I, O any](h Handler[I, O], pred func(I) bool) Handler[I, O] {
	return func(ctx context.Context, input I) (O, error) {
		if !pred(input) {
			var zero O
			return zero, nil
		}
		return h(ctx, input)
	}
}

type Bus[T any] struct {
	ch       chan T
	handlers []Handler[T, struct{}]
}

func NewBus[T any](buf int) *Bus[T] {
	return &Bus[T]{ch: make(chan T, buf)}
}

func (b *Bus[T]) Subscribe(h Handler[T, struct{}]) {
	b.handlers = append(b.handlers, h)
}

func (b *Bus[T]) Publish(payload T) {
	select {
	case b.ch <- payload:
	default:
	}
}

func (b *Bus[T]) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-b.ch:
			for _, h := range b.handlers {
				h(ctx, p) //nolint:errcheck
			}
		}
	}
}

// ============================================================================
// Session state
// ============================================================================

type SessionState int32

const (
	StateIdle         SessionState = iota
	StateListening                 // mic active, forwarding to Gemini
	StateSpeaking                  // playing Gemini audio
	StateExecutingDSL              // DSL pipeline running
)

func (s SessionState) String() string {
	return [...]string{"idle", "listening", "speaking", "executing_dsl"}[s]
}

// ============================================================================
// Audio constants
// ============================================================================

const (
	sampleRateIn  = 16000 // Gemini expects 16kHz PCM16
	sampleRateOut = 24000 // Gemini returns 24kHz PCM16
	framesPerBuf  = 4096
)

// ============================================================================
// Session
// ============================================================================

type Session struct {
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *slog.Logger
	runtime *agentscript.Runtime
	gem     *gl.LiveSession
	sm      *StateMachine[SessionState]

	// typed event buses
	audioInBus  *Bus[[]byte] // raw PCM16 from mic → Gemini
	audioOutBus *Bus[[]byte] // raw PCM16 from Gemini → speaker

	// playback queue
	playMu     sync.Mutex
	playQueue  [][]byte
	playNotify chan struct{}

	// mic gate
	muted atomic.Bool
}

func newSession(ctx context.Context, cancel context.CancelFunc, runtime *agentscript.Runtime, logger *slog.Logger) *Session {
	s := &Session{
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
		runtime:     runtime,
		audioInBus:  NewBus[[]byte](64),
		audioOutBus: NewBus[[]byte](128),
		playNotify:  make(chan struct{}, 1),
	}
	s.sm = NewStateMachine[SessionState](StateIdle, logger)
	s.registerTransitions()
	return s
}

func (s *Session) registerTransitions() {
	print := func(msg string) func() {
		return func() { fmt.Println(msg) }
	}
	s.sm.Register(StateIdle, StateListening, print("🎤 Listening..."))
	s.sm.Register(StateListening, StateSpeaking, print("🔊 Speaking..."))
	s.sm.Register(StateListening, StateExecutingDSL, print("⚙️  Executing DSL..."))
	s.sm.Register(StateSpeaking, StateListening, print("🎤 Listening..."))
	s.sm.Register(StateSpeaking, StateExecutingDSL, print("⚙️  Executing DSL..."))
	s.sm.Register(StateExecutingDSL, StateSpeaking, print("🔊 Speaking..."))
	s.sm.Register(StateExecutingDSL, StateListening, print("🎤 Listening..."))
}

// ============================================================================
// Gemini wiring
// ============================================================================

func (s *Session) connect() error {
	var toolDef struct {
		Name        string        `json:"name"`
		Description string        `json:"description"`
		Parameters  *genai.Schema `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(AgentScriptToolJSON), &toolDef); err != nil {
		return fmt.Errorf("parse tool definition: %w", err)
	}

	gem, err := gl.NewLiveSession(s.ctx, gl.LiveConfig{
		SystemPrompt: MorpheusAgentSystemPrompt,
		VoiceName:    "Puck",
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        toolDef.Name,
				Description: toolDef.Description,
				Parameters:  toolDef.Parameters,
			}},
		}},
	}, s.logger)
	if err != nil {
		return fmt.Errorf("connect to Gemini Live: %w", err)
	}
	s.gem = gem

	// Gemini audio → playback queue
	s.gem.OnAudio = func(audio []byte) {
		s.sm.Transition(StateSpeaking)
		s.enqueueAudio(audio)
	}

	s.gem.OnText = func(text string) {
		fmt.Printf("💬 %s\n", text)
	}

	s.gem.OnToolCall = func(id, name string, args map[string]any) {
		if name == "agentscript_dsl" {
			go s.executeDSL(id, args)
		}
	}

	// Barge-in: user spoke while Gemini was speaking — drain playback queue
	s.gem.OnInterrupt = func() {
		fmt.Println("⏸  Interrupted")
		s.drainAudio()
		s.sm.Transition(StateListening)
	}

	s.gem.OnTurnDone = func() {
		// wait for playback to finish, then go back to listening
		go func() {
			s.waitPlaybackDone()
			s.sm.Transition(StateListening)
		}()
	}

	// Wire audioInBus: always forward mic → Gemini
	// Gemini's own VAD handles barge-in; we don't gate on state here
	s.audioInBus.Subscribe(Handler[[]byte, struct{}](func(ctx context.Context, pcm []byte) (struct{}, error) {
		if err := s.gem.SendAudio(pcm); err != nil {
			s.logger.Debug("send audio failed", "error", err)
		}
		return struct{}{}, nil
	}))

	go s.audioInBus.Run(s.ctx)
	go s.audioOutBus.Run(s.ctx)
	s.gem.StartReceiving(s.ctx)
	s.sm.Transition(StateListening)
	return nil
}

// ============================================================================
// Audio playback queue — sequential, interruptible
// ============================================================================

func (s *Session) enqueueAudio(pcm []byte) {
	s.playMu.Lock()
	s.playQueue = append(s.playQueue, pcm)
	s.playMu.Unlock()
	select {
	case s.playNotify <- struct{}{}:
	default:
	}
}

func (s *Session) drainAudio() {
	s.playMu.Lock()
	s.playQueue = nil
	s.playMu.Unlock()
}

func (s *Session) waitPlaybackDone() {
	for {
		s.playMu.Lock()
		empty := len(s.playQueue) == 0
		s.playMu.Unlock()
		if empty {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// runPlayback is the portaudio output stream callback loop.
// It drains playQueue sequentially into the speaker.
func (s *Session) runPlayback() error {
	var buf []int16
	bufPos := 0

	stream, err := portaudio.OpenDefaultStream(
		0, 1, float64(sampleRateOut), framesPerBuf,
		func(out []int16) {
			for i := range out {
				if bufPos < len(buf) {
					out[i] = buf[bufPos]
					bufPos++
				} else {
					// try to grab next chunk from queue
					s.playMu.Lock()
					if len(s.playQueue) > 0 {
						chunk := s.playQueue[0]
						s.playQueue = s.playQueue[1:]
						s.playMu.Unlock()
						// convert []byte PCM16-LE to []int16
						buf = make([]int16, len(chunk)/2)
						for j := 0; j < len(buf); j++ {
							buf[j] = int16(chunk[j*2]) | int16(chunk[j*2+1])<<8
						}
						bufPos = 0
						if bufPos < len(buf) {
							out[i] = buf[bufPos]
							bufPos++
						} else {
							out[i] = 0
						}
					} else {
						s.playMu.Unlock()
						out[i] = 0
					}
				}
			}
		},
	)
	if err != nil {
		return fmt.Errorf("open output stream: %w", err)
	}
	defer stream.Close()
	if err := stream.Start(); err != nil {
		return fmt.Errorf("start output stream: %w", err)
	}
	<-s.ctx.Done()
	stream.Stop()
	return nil
}

// runMic captures mic audio and publishes to audioInBus.
func (s *Session) runMic() error {
	buf := make([]int16, framesPerBuf)

	stream, err := portaudio.OpenDefaultStream(
		1, 0, float64(sampleRateIn), framesPerBuf, buf,
	)
	if err != nil {
		return fmt.Errorf("open input stream: %w", err)
	}
	defer stream.Close()
	if err := stream.Start(); err != nil {
		return fmt.Errorf("start input stream: %w", err)
	}

	for {
		select {
		case <-s.ctx.Done():
			stream.Stop()
			return nil
		default:
		}
		if err := stream.Read(); err != nil {
			s.logger.Debug("mic read error", "error", err)
			continue
		}
		// convert []int16 → []byte PCM16-LE
		pcm := make([]byte, len(buf)*2)
		for i, v := range buf {
			pcm[i*2] = byte(v)
			pcm[i*2+1] = byte(v >> 8)
		}
		if !s.muted.Load() {
			s.audioInBus.Publish(pcm)
		}
	}
}

// ============================================================================
// DSL execution
// ============================================================================

func (s *Session) executeDSL(callID string, args map[string]any) {
	dslArg, ok := args["dsl"]
	if !ok {
		s.sendToolResponse(callID, map[string]any{"error": "missing dsl parameter"})
		return
	}
	dsl, ok := dslArg.(string)
	if !ok {
		s.sendToolResponse(callID, map[string]any{"error": "dsl must be string"})
		return
	}

	fmt.Printf("⚡ DSL: %s\n", dsl)
	s.sm.Transition(StateExecutingDSL)
	s.sendToolResponse(callID, map[string]any{"status": "executing", "dsl": dsl})

	program, err := agentscript.Parse(dsl)
	if err != nil {
		errMsg := fmt.Sprintf("DSL parse error: %v", err)
		fmt.Printf("❌ %s\n", errMsg)
		s.sm.Transition(StateListening)
		s.gem.SendText("I couldn't parse the DSL pipeline: " + errMsg)
		return
	}

	result, err := s.runtime.Execute(s.ctx, program)
	if err != nil {
		errMsg := fmt.Sprintf("DSL execution error: %v", err)
		fmt.Printf("❌ %s\n", errMsg)
		s.sm.Transition(StateListening)
		s.gem.SendText("The DSL pipeline failed: " + errMsg)
		return
	}

	fmt.Printf("✅ DSL done (%d bytes)\n", len(result))
	s.sm.Transition(StateSpeaking)
	s.gem.SendText(fmt.Sprintf("DSL pipeline completed. Results:\n\n%s\n\nPlease summarize concisely for voice.", result))
}

func (s *Session) sendToolResponse(id string, result map[string]any) {
	if err := s.gem.SendToolResponse(id, result); err != nil {
		s.logger.Debug("send tool response failed", "error", err)
	}
}

// ============================================================================
// Screenshot
// ============================================================================

func (s *Session) takeScreenshot() {
	fmt.Println("📸 Taking screenshot...")
	cmd := exec.Command("screencapture", "-t", "jpg", "-x", "/tmp/morpheus_screenshot.jpg")
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Screenshot failed: %v\n", err)
		return
	}
	data, err := os.ReadFile("/tmp/morpheus_screenshot.jpg")
	if err != nil {
		fmt.Printf("❌ Read screenshot failed: %v\n", err)
		return
	}
	imageB64 := base64.StdEncoding.EncodeToString(data)
	if err := s.gem.SendImage(imageB64); err != nil {
		fmt.Printf("❌ Send screenshot failed: %v\n", err)
		return
	}
	fmt.Println("📸 Screenshot sent")
	s.gem.SendText("I just took a screenshot of my screen. What do you see and what actions can you help with?")
	os.Remove("/tmp/morpheus_screenshot.jpg")
}

// ============================================================================
// Main
// ============================================================================

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		log.Fatal("GEMINI_API_KEY not set")
	}

	googleCreds := os.Getenv("GOOGLE_CREDENTIALS_FILE")
	if googleCreds == "" {
		if _, err := os.Stat("credentials.json"); err == nil {
			googleCreds = "credentials.json"
		}
	}
	searchKey := os.Getenv("SEARCH_API_KEY")
	if searchKey == "" {
		searchKey = os.Getenv("SERPAPI_KEY")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtime, err := agentscript.NewRuntime(ctx, agentscript.RuntimeConfig{
		GeminiAPIKey:       geminiKey,
		ClaudeAPIKey:       os.Getenv("CLAUDE_API_KEY"),
		SearchAPIKey:       searchKey,
		GoogleCredsFile:    googleCreds,
		GoogleTokenFile:    os.Getenv("GOOGLE_TOKEN_FILE"),
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubTokenFile:    os.Getenv("GITHUB_TOKEN_FILE"),
		Verbose:            false,
	})
	if err != nil {
		log.Fatal("failed to create runtime:", err)
	}

	// Init portaudio
	if err := portaudio.Initialize(); err != nil {
		log.Fatal("portaudio init failed:", err)
	}
	defer portaudio.Terminate()

	sess := newSession(ctx, cancel, runtime, logger)

	fmt.Println("🚀 Morpheus — AgentScript Live")
	fmt.Println("   Connecting to Gemini Live...")

	if err := sess.connect(); err != nil {
		log.Fatal("connect failed:", err)
	}
	fmt.Println("🟢 Connected. Controls: [SPACE] mute/unmute  [S] screenshot  [Ctrl+C] quit")

	// Handle signals
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Run mic and playback concurrently
	errCh := make(chan error, 2)
	go func() { errCh <- sess.runMic() }()
	go func() { errCh <- sess.runPlayback() }()

	// Raw terminal — single keypress without Enter
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Println("⚠️  Could not set raw terminal, using line mode (press Enter after key)")
	}
	go func() {
		buf := make([]byte, 1)
		for {
			n, _ := os.Stdin.Read(buf)
			if n == 0 {
				continue
			}
			switch buf[0] {
			case ' ':
				if sess.muted.Load() {
					sess.muted.Store(false)
					fmt.Println("\r🎤 Unmuted")
				} else {
					sess.muted.Store(true)
					fmt.Println("\r🔇 Muted")
				}
			case 's', 'S':
				go sess.takeScreenshot()
			case 3: // Ctrl+C
				cancel()
				return
			}
		}
	}()

	select {
	case <-sig:
		fmt.Println("\n👋 Shutting down...")
		cancel()
	case err := <-errCh:
		if err != nil {
			fmt.Printf("❌ Audio error: %v\n", err)
		}
		cancel()
	}

	if oldState != nil {
		term.Restore(int(os.Stdin.Fd()), oldState)
	}
	sess.gem.Close()
}

// ============================================================================
// Constants
// ============================================================================

const (
	MorpheusAgentSystemPrompt = `You are Morpheus, a voice AI agent. Execute tasks using AgentScript DSL by calling the "agentscript_dsl" tool.

DSL GRAMMAR - FOLLOW EXACTLY, NO VARIATIONS:
  Syntax: command "arg1" "arg2"
  NO dots. NO parens. NO equals signs. NO semicolons.

SEQUENTIAL (pipe output into next command):
  command1 "arg" >=> command2 "arg"

PARALLEL (run simultaneously):
  ( command1 "arg" <*> command2 "arg" ) >=> merge

VALID COMMANDS - USE ONLY THESE EXACT NAMES:
  weather "city"
  crypto "SYMBOL"
  search "query"
  news "topic"
  stock "TICKER"
  job_search "role" "location"
  places_search "query"
  maps_trip "title"
  reddit "subreddit"
  rss "url"
  ask "question"
  summarize
  analyze "focus"
  save "filename"
  read "filename"
  email "address"
  notify "channel"
  translate "language"
  image_generate "prompt"
  text_to_speech "text"
  merge

CORRECT EXAMPLES:
  weather "New York"
  weather "London" >=> ask "Summarize in one sentence"
  ( weather "NYC" <*> crypto "BTC" ) >=> merge >=> ask "Summarize for voice"
  job_search "golang developer" "remote"
  search "India vs NZ cricket score" >=> ask "What is the current score?"
  news "AI" >=> summarize

WRONG - NEVER DO THIS:
  weather.current_weather(city="New York")   <- no dots or parens
  weather city="New York"                    <- no equals signs
  cmd1; cmd2                                 <- use >=> not semicolons

RULES:
- For current info (weather, news, prices) -> ALWAYS use DSL
- For simple conversational questions -> answer directly without DSL
- After DSL completes, respond conversationally based on results`

	AgentScriptToolJSON = `{
		"name": "agentscript_dsl",
		"description": "Execute AgentScript DSL pipeline. Syntax: command \"arg\" >=> command \"arg\". NO dots, NO parens, NO equals signs.",
		"parameters": {
			"type": "object",
			"properties": {
				"dsl": {
					"type": "string",
					"description": "AgentScript DSL. Example: weather \"NYC\" or ( weather \"NYC\" <*> crypto \"BTC\" ) >=> merge >=> ask \"Summarize\""
				}
			},
			"required": ["dsl"]
		}
	}`
)
