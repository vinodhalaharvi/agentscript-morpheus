package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	gl "github.com/vinodhalaharvi/agentscript/pkg/geminilive"

	"github.com/gorilla/websocket"
	"github.com/vinodhalaharvi/agentscript/internal/agentscript"
	"google.golang.org/genai"
)

const (
	// System prompt for Morpheus Agent with AgentScript DSL
	MorpheusAgentSystemPrompt = `You are Morpheus, a programmable AI agent with voice and vision. You can see through the user's camera, hear their voice, and execute complex multi-step workflows using AgentScript DSL.

CRITICAL: When users ask questions requiring data, search, or automation, compose AgentScript DSL pipelines and call the "agentscript_dsl" tool.

AVAILABLE DSL COMMANDS:
  search, ask, summarize, analyze, save, read, list, merge
  weather, crypto, reddit, rss, news, stock, job_search
  email, calendar, notify, places_search, maps_trip
  image_generate, text_to_speech, translate

DSL OPERATORS:
  >=>  Sequential: command1 >=> command2 (output of cmd1 feeds cmd2)
  <*>  Parallel: ( cmd1 <*> cmd2 <*> cmd3 ) (run all in parallel)
  ( )  Grouping: Group parallel operations

EXAMPLES:

User: "What's the weather in NYC and Bitcoin price?"
→ Call agentscript_dsl with:
  dsl: '( weather "NYC" <*> crypto "BTC" ) >=> merge >=> ask "Summarize weather and crypto in 2 sentences"'

User: "Search for restaurants near me and put them on a map"  
→ Call agentscript_dsl with:
  dsl: 'places_search "restaurants near Reston VA" >=> maps_trip "Restaurants Near Me"'

User: "Find golang jobs and email me the results"
→ Call agentscript_dsl with:
  dsl: 'job_search "golang developer" "remote" >=> ask "Format as table with company, role, salary" >=> email "user@gmail.com"'

User: "Get India vs NZ cricket score and notify me on Slack"
→ Call agentscript_dsl with:
  dsl: 'search "India vs New Zealand live cricket score" >=> ask "Extract current score and status" >=> notify "slack"'

RULES:
- For current info (weather, news, scores, prices) → ALWAYS use DSL
- For data analysis or automation → ALWAYS use DSL  
- For simple facts you know → answer directly
- Keep DSL concise (2-5 commands max)
- Use >=> ask "..." >=> to format results for speech
- After DSL execution, respond conversationally based on results

When you see images through camera, describe what you see and offer DSL actions if relevant.`

	// Tool definition for AgentScript DSL execution
	AgentScriptToolJSON = `{
		"name": "agentscript_dsl",
		"description": "Execute AgentScript DSL pipeline for data fetching, analysis, automation. Use >=> for sequential, <*> for parallel operations.",
		"parameters": {
			"type": "object",
			"properties": {
				"dsl": {
					"type": "string",
					"description": "AgentScript DSL command. Example: weather \"NYC\" >=> ask \"Summarize for voice\" or ( search \"news\" <*> crypto \"BTC\" ) >=> merge"
				}
			},
			"required": ["dsl"]
		}
	}`
)

// Server handles WebSocket connections and Gemini Live integration
type Server struct {
	upgrader websocket.Upgrader
	runtime  *agentscript.Runtime
	logger   *slog.Logger
}

// UserSession manages a single user's live session
type UserSession struct {
	conn          *websocket.Conn
	geminiSession *gl.LiveSession // Use the imported LiveSession from live.go
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *slog.Logger
	runtime       *agentscript.Runtime
	mu            sync.Mutex
}

// WebSocket message types
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

// Audio message from browser
type AudioMessage struct {
	Audio string `json:"audio"` // base64 PCM16
}

// Image message from browser (screenshot)
type ImageMessage struct {
	Image string `json:"image"` // base64 JPEG
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Initialize AgentScript runtime
	cfg := agentscript.RuntimeConfig{
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		ClaudeAPIKey: os.Getenv("CLAUDE_API_KEY"),
		Verbose:      true,
	}

	ctx := context.Background()
	runtime, err := agentscript.NewRuntime(ctx, cfg)
	if err != nil {
		log.Fatal("Failed to create runtime:", err)
	}

	server := &Server{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		runtime: runtime,
		logger:  logger,
	}

	// Serve static files
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/ws", server.handleWebSocket)

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	logger.Info("Starting Gemini Live server", "port", port)

	// Handle shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.Info("Shutting down...")
		os.Exit(0)
	}()

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := &UserSession{
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		logger:  s.logger,
		runtime: s.runtime,
	}

	// Initialize Gemini Live session
	if err := session.initGeminiLive(); err != nil {
		s.logger.Error("failed to init gemini live", "error", err)
		session.sendError("Failed to connect to Gemini Live")
		return
	}
	defer session.closeGeminiLive()

	// Send ready status
	session.sendStatus("live")

	// Handle WebSocket messages
	session.handleMessages()
}

func (us *UserSession) initGeminiLive() error {
	// Parse tool definition
	var toolDef map[string]interface{}
	if err := json.Unmarshal([]byte(AgentScriptToolJSON), &toolDef); err != nil {
		return fmt.Errorf("parse tool definition: %w", err)
	}

	tool := &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        toolDef["name"].(string),
			Description: toolDef["description"].(string),
			Parameters:  toolDef["parameters"].(map[string]interface{}),
		}},
	}

	config := gl.LiveConfig{
		SystemPrompt: MorpheusAgentSystemPrompt,
		VoiceName:    "Puck", // or "Charon", "Kore", "Fenrir"
		Tools:        []*genai.Tool{tool},
	}

	// Create LiveSession using the imported implementation
	session, err := gl.NewLiveSession(us.ctx, config, us.logger)
	if err != nil {
		return fmt.Errorf("create live session: %w", err)
	}

	us.geminiSession = session

	// Set up event handlers
	us.geminiSession.OnAudio = func(audio []byte) {
		audioB64 := base64.StdEncoding.EncodeToString(audio)
		us.sendMessage(WSMessage{
			Type: "audio_out",
			Data: map[string]string{"audio": audioB64},
		})
	}

	us.geminiSession.OnText = func(text string) {
		us.sendMessage(WSMessage{
			Type: "transcript",
			Data: map[string]string{"text": text},
		})
	}

	us.geminiSession.OnToolCall = func(id, name string, args map[string]any) {
		if name == "agentscript_dsl" {
			go us.executeDSL(id, args)
		}
	}

	us.geminiSession.OnInterrupt = func() {
		us.sendMessage(WSMessage{Type: "interrupted"})
	}

	us.geminiSession.OnTurnDone = func() {
		us.sendMessage(WSMessage{Type: "turn_complete"})
	}

	// Start receiving from Gemini
	us.geminiSession.StartReceiving(us.ctx)

	return nil
}

func (us *UserSession) executeDSL(callID string, args map[string]interface{}) {
	dslArg, ok := args["dsl"]
	if !ok {
		us.sendToolResponse(callID, map[string]interface{}{
			"error": "missing dsl parameter",
		})
		return
	}

	dsl, ok := dslArg.(string)
	if !ok {
		us.sendToolResponse(callID, map[string]interface{}{
			"error": "dsl parameter must be string",
		})
		return
	}

	us.logger.Info("executing DSL", "dsl", dsl, "call_id", callID)

	// Send placeholder response to unblock audio
	us.sendToolResponse(callID, map[string]interface{}{
		"status": "executing DSL pipeline...",
		"dsl":    dsl,
	})

	// Parse and execute DSL
	program, err := agentscript.Parse(dsl)
	if err != nil {
		result := map[string]interface{}{
			"error": fmt.Sprintf("DSL parse error: %v", err),
			"dsl":   dsl,
		}
		us.sendToolResponse(callID, result)
		return
	}

	result, err := us.runtime.Execute(us.ctx, program)
	if err != nil {
		errorResult := map[string]interface{}{
			"error": fmt.Sprintf("DSL execution error: %v", err),
			"dsl":   dsl,
		}
		us.sendToolResponse(callID, errorResult)
		return
	}

	// Send final result
	finalResponse := map[string]interface{}{
		"success": true,
		"result":  result,
		"dsl":     dsl,
		"summary": fmt.Sprintf("Successfully executed: %s", dsl),
	}

	us.sendToolResponse(callID, finalResponse)

	// Inject result back to Gemini for vocalization
	us.geminiSession.SendText(fmt.Sprintf("DSL execution completed successfully. Here are the results: %s", result))

	us.logger.Info("DSL executed successfully", "dsl", dsl, "result_length", len(result))
}

func (us *UserSession) sendToolResponse(id string, result map[string]interface{}) {
	if err := us.geminiSession.SendToolResponse(id, result); err != nil {
		us.logger.Error("send tool response failed", "error", err)
	}
}

func (us *UserSession) handleMessages() {
	for {
		var msg WSMessage
		if err := us.conn.ReadJSON(&msg); err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				return
			}
			us.logger.Error("websocket read error", "error", err)
			return
		}

		switch msg.Type {
		case "audio":
			us.handleAudio(msg.Data)
		case "image":
			us.handleImage(msg.Data)
		case "text":
			us.handleText(msg.Data)
		case "screenshot":
			us.takeScreenshot()
		default:
			us.logger.Warn("unknown message type", "type", msg.Type)
		}
	}
}

func (us *UserSession) handleAudio(data interface{}) {
	audioData, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	audioB64, ok := audioData["audio"].(string)
	if !ok {
		return
	}

	pcm16, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		us.logger.Error("decode audio failed", "error", err)
		return
	}

	// Send audio to Gemini
	if err := us.geminiSession.SendAudio(pcm16); err != nil {
		us.logger.Error("send audio to gemini failed", "error", err)
	}
}

func (us *UserSession) handleImage(data interface{}) {
	imageData, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	imageB64, ok := imageData["image"].(string)
	if !ok {
		return
	}

	// Send image to Gemini
	if err := us.geminiSession.SendImage(imageB64); err != nil {
		us.logger.Error("send image to gemini failed", "error", err)
	}
}

func (us *UserSession) handleText(data interface{}) {
	textData, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	text, ok := textData["text"].(string)
	if !ok {
		return
	}

	us.geminiSession.SendText(text)
}

func (us *UserSession) takeScreenshot() {
	// Take screenshot using screencapture on macOS
	cmd := exec.Command("screencapture", "-t", "jpg", "-x", "/tmp/screenshot.jpg")
	if err := cmd.Run(); err != nil {
		us.logger.Error("screenshot failed", "error", err)
		return
	}

	// Read screenshot
	data, err := os.ReadFile("/tmp/screenshot.jpg")
	if err != nil {
		us.logger.Error("read screenshot failed", "error", err)
		return
	}

	// Convert to base64 and send
	imageB64 := base64.StdEncoding.EncodeToString(data)
	if err := us.geminiSession.SendImage(imageB64); err != nil {
		us.logger.Error("send screenshot failed", "error", err)
	}

	// Also send context
	us.geminiSession.SendText("I just took a screenshot of my current screen. What do you see and what actions can you help me with?")

	// Clean up
	os.Remove("/tmp/screenshot.jpg")
}

func (us *UserSession) sendMessage(msg WSMessage) {
	us.mu.Lock()
	defer us.mu.Unlock()
	if err := us.conn.WriteJSON(msg); err != nil {
		us.logger.Error("websocket write failed", "error", err)
	}
}

func (us *UserSession) sendStatus(status string) {
	us.sendMessage(WSMessage{Type: "status", Data: status})
}

func (us *UserSession) sendError(error string) {
	us.sendMessage(WSMessage{Type: "error", Data: error})
}

func (us *UserSession) closeGeminiLive() {
	if us.geminiSession != nil {
		us.geminiSession.Close()
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>AgentScript Live</title>
    <meta charset="utf-8">
    <style>
        body { font-family: Arial, sans-serif; padding: 20px; background: #1a1a1a; color: #fff; }
        .container { max-width: 800px; margin: 0 auto; }
        .status { padding: 10px; border-radius: 5px; margin: 10px 0; }
        .live { background: #2d5016; }
        .connecting { background: #ffa500; color: #000; }
        .error { background: #8b0000; }
        button { padding: 10px 20px; margin: 5px; border: none; border-radius: 5px; cursor: pointer; }
        .primary { background: #4CAF50; color: white; }
        .secondary { background: #2196F3; color: white; }
        .danger { background: #f44336; color: white; }
        #transcript { background: #333; padding: 15px; border-radius: 5px; min-height: 200px; margin: 10px 0; }
        #controls { text-align: center; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 AgentScript Live</h1>
        <p>Voice + Vision AI Agent with Morpheus DSL</p>
        
        <div id="status" class="status connecting">Connecting...</div>
        
        <div id="controls">
            <button id="connectBtn" class="primary">Connect</button>
            <button id="micBtn" class="secondary" disabled>🎤 Start Mic</button>
            <button id="screenshotBtn" class="secondary" disabled>📸 Screenshot</button>
            <button id="cameraBtn" class="secondary" disabled>📹 Camera</button>
        </div>
        
        <div id="transcript">
            <p>Ready to assist! Try saying:</p>
            <ul>
                <li>"What's the weather in New York?"</li>
                <li>"Search for golang jobs and email me"</li>
                <li>"Take a screenshot and tell me what you see"</li>
                <li>"Find restaurants near me and put them on a map"</li>
            </ul>
        </div>
    </div>

    <script>
        let ws = null;
        let mediaRecorder = null;
        let audioChunks = [];
        let isRecording = false;

        const statusDiv = document.getElementById('status');
        const transcriptDiv = document.getElementById('transcript');
        const connectBtn = document.getElementById('connectBtn');
        const micBtn = document.getElementById('micBtn');
        const screenshotBtn = document.getElementById('screenshotBtn');
        const cameraBtn = document.getElementById('cameraBtn');

        connectBtn.addEventListener('click', connect);
        micBtn.addEventListener('click', toggleMic);
        screenshotBtn.addEventListener('click', takeScreenshot);
        cameraBtn.addEventListener('click', startCamera);

        function connect() {
            ws = new WebSocket('ws://localhost:8080/ws');
            
            ws.onopen = () => {
                updateStatus('Connecting to Gemini Live...', 'connecting');
                connectBtn.disabled = true;
            };
            
            ws.onmessage = (event) => {
                const msg = JSON.parse(event.data);
                handleMessage(msg);
            };
            
            ws.onclose = () => {
                updateStatus('Disconnected', 'error');
                connectBtn.disabled = false;
                micBtn.disabled = true;
                screenshotBtn.disabled = true;
                cameraBtn.disabled = true;
            };
        }

        function handleMessage(msg) {
            switch(msg.type) {
                case 'status':
                    if(msg.data === 'live') {
                        updateStatus('🟢 Connected - Ready for voice!', 'live');
                        micBtn.disabled = false;
                        screenshotBtn.disabled = false;
                        cameraBtn.disabled = false;
                    }
                    break;
                case 'transcript':
                    addToTranscript('🤖 ' + msg.data.text);
                    break;
                case 'audio_out':
                    playAudio(msg.data.audio);
                    break;
                case 'error':
                    addToTranscript('❌ Error: ' + msg.data);
                    break;
                case 'interrupted':
                    addToTranscript('⏸️ Interrupted');
                    break;
            }
        }

        function updateStatus(text, className) {
            statusDiv.textContent = text;
            statusDiv.className = 'status ' + className;
        }

        function addToTranscript(text) {
            transcriptDiv.innerHTML += '<p>' + text + '</p>';
            transcriptDiv.scrollTop = transcriptDiv.scrollHeight;
        }

        async function toggleMic() {
            if (!isRecording) {
                try {
                    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
                    mediaRecorder = new MediaRecorder(stream);
                    audioChunks = [];

                    mediaRecorder.ondataavailable = (event) => {
                        audioChunks.push(event.data);
                    };

                    mediaRecorder.onstop = () => {
                        const audioBlob = new Blob(audioChunks, { type: 'audio/wav' });
                        const reader = new FileReader();
                        reader.onload = () => {
                            const arrayBuffer = reader.result;
                            // Convert to base64 for sending
                            const base64 = btoa(String.fromCharCode(...new Uint8Array(arrayBuffer)));
                            ws.send(JSON.stringify({
                                type: 'audio',
                                data: { audio: base64 }
                            }));
                        };
                        reader.readAsArrayBuffer(audioBlob);
                    };

                    mediaRecorder.start();
                    isRecording = true;
                    micBtn.textContent = '🔴 Stop Mic';
                    addToTranscript('🎤 Listening...');
                } catch (err) {
                    console.error('Microphone access denied:', err);
                    addToTranscript('❌ Microphone access denied');
                }
            } else {
                mediaRecorder.stop();
                mediaRecorder.stream.getTracks().forEach(track => track.stop());
                isRecording = false;
                micBtn.textContent = '🎤 Start Mic';
            }
        }

        function takeScreenshot() {
            ws.send(JSON.stringify({
                type: 'screenshot'
            }));
            addToTranscript('📸 Taking screenshot...');
        }

        async function startCamera() {
            try {
                const stream = await navigator.mediaDevices.getUserMedia({ video: true });
                const video = document.createElement('video');
                video.srcObject = stream;
                video.play();

                video.onloadedmetadata = () => {
                    const canvas = document.createElement('canvas');
                    canvas.width = video.videoWidth;
                    canvas.height = video.videoHeight;
                    const ctx = canvas.getContext('2d');
                    ctx.drawImage(video, 0, 0);
                    
                    canvas.toBlob((blob) => {
                        const reader = new FileReader();
                        reader.onload = () => {
                            const base64 = reader.result.split(',')[1];
                            ws.send(JSON.stringify({
                                type: 'image',
                                data: { image: base64 }
                            }));
                            addToTranscript('📹 Camera frame sent');
                        };
                        reader.readAsDataURL(blob);
                    }, 'image/jpeg');
                    
                    stream.getTracks().forEach(track => track.stop());
                };
            } catch (err) {
                console.error('Camera access denied:', err);
                addToTranscript('❌ Camera access denied');
            }
        }

        function playAudio(base64Audio) {
            const audioData = atob(base64Audio);
            const arrayBuffer = new ArrayBuffer(audioData.length);
            const uint8Array = new Uint8Array(arrayBuffer);
            for (let i = 0; i < audioData.length; i++) {
                uint8Array[i] = audioData.charCodeAt(i);
            }
            
            const audioContext = new (window.AudioContext || window.webkitAudioContext)();
            audioContext.decodeAudioData(arrayBuffer).then(audioBuffer => {
                const source = audioContext.createBufferSource();
                source.buffer = audioBuffer;
                source.connect(audioContext.destination);
                source.start();
            });
        }

        // Auto-connect on load
        window.onload = () => connect();
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}
