package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const baseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiClient is a simple HTTP client for the Gemini API
type GeminiClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGeminiClient creates a new Gemini client
func NewGeminiClient(apiKey, model string) *GeminiClient {
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiClient{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
	}
}

// Request structures
type generateRequest struct {
	Contents         []content         `json:"contents"`
	GenerationConfig *generationConfig `json:"generationConfig,omitempty"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inline_data,omitempty"`
	FileData   *fileData   `json:"file_data,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type fileData struct {
	MimeType string `json:"mime_type"`
	FileURI  string `json:"file_uri"`
}

type generationConfig struct {
	ResponseMimeType string `json:"response_mime_type,omitempty"`
}

// Response structures
type generateResponse struct {
	Candidates []candidate `json:"candidates"`
	Error      *apiError   `json:"error,omitempty"`
}

type candidate struct {
	Content content `json:"content"`
}

type apiError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// GenerateContent sends a prompt to Gemini and returns the response text
func (c *GeminiClient) GenerateContent(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, c.model, c.apiKey)

	reqBody := generateRequest{
		Contents: []content{
			{
				Parts: []part{
					{Text: prompt},
				},
			},
		},
	}

	return c.doRequest(ctx, url, reqBody)
}

// AnalyzeImage analyzes an image file with a prompt
func (c *GeminiClient) AnalyzeImage(ctx context.Context, imagePath, prompt string) (string, error) {
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, c.model, c.apiKey)

	// Read and encode image
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image: %w", err)
	}

	mimeType := getMimeType(imagePath)
	encoded := base64.StdEncoding.EncodeToString(imageData)

	reqBody := generateRequest{
		Contents: []content{
			{
				Parts: []part{
					{
						InlineData: &inlineData{
							MimeType: mimeType,
							Data:     encoded,
						},
					},
					{Text: prompt},
				},
			},
		},
	}

	return c.doRequest(ctx, url, reqBody)
}

// AnalyzeVideo analyzes a video with a prompt (using File API for larger files)
func (c *GeminiClient) AnalyzeVideo(ctx context.Context, videoPath, prompt string) (string, error) {
	// For videos, we need to upload to File API first, then reference
	// For now, support small videos via inline data (< 20MB)

	fileInfo, err := os.Stat(videoPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat video: %w", err)
	}

	// Check file size (limit to 20MB for inline)
	if fileInfo.Size() > 20*1024*1024 {
		return "", fmt.Errorf("video too large for inline processing (max 20MB). Use File API for larger videos")
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, c.model, c.apiKey)

	// Read and encode video
	videoData, err := os.ReadFile(videoPath)
	if err != nil {
		return "", fmt.Errorf("failed to read video: %w", err)
	}

	mimeType := getMimeType(videoPath)
	encoded := base64.StdEncoding.EncodeToString(videoData)

	reqBody := generateRequest{
		Contents: []content{
			{
				Parts: []part{
					{
						InlineData: &inlineData{
							MimeType: mimeType,
							Data:     encoded,
						},
					},
					{Text: prompt},
				},
			},
		},
	}

	return c.doRequest(ctx, url, reqBody)
}

// GenerateImage generates an image using Imagen model
func (c *GeminiClient) GenerateImage(ctx context.Context, prompt string) ([]byte, error) {
	// Use Imagen 4 - Imagen 3 has been shut down
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/imagen-4.0-generate-001:predict?key=%s", c.apiKey)

	reqBody := map[string]interface{}{
		"instances": []map[string]string{
			{"prompt": prompt},
		},
		"parameters": map[string]interface{}{
			"sampleCount": 1,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != 200 {
		var errResp struct {
			Error *apiError `json:"error,omitempty"`
		}
		json.Unmarshal(body, &errResp)
		if errResp.Error != nil {
			return nil, fmt.Errorf("API error: %s (code %d)", errResp.Error.Message, errResp.Error.Code)
		}
		return nil, fmt.Errorf("API error: status %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var imgResp struct {
		Predictions []struct {
			BytesBase64Encoded string `json:"bytesBase64Encoded"`
		} `json:"predictions"`
	}

	if err := json.Unmarshal(body, &imgResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(imgResp.Predictions) == 0 || imgResp.Predictions[0].BytesBase64Encoded == "" {
		return nil, fmt.Errorf("no image generated")
	}

	// Decode base64 image
	imageBytes, err := base64.StdEncoding.DecodeString(imgResp.Predictions[0].BytesBase64Encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return imageBytes, nil
}

func (c *GeminiClient) doRequest(ctx context.Context, url string, reqBody generateRequest) (string, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var genResp generateResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if genResp.Error != nil {
		return "", fmt.Errorf("API error: %s (code %d)", genResp.Error.Message, genResp.Error.Code)
	}

	if len(genResp.Candidates) == 0 || len(genResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	return genResp.Candidates[0].Content.Parts[0].Text, nil
}

func getMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mpeg":
		return "video/mpeg"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}

// GenerateVideo generates a video using Veo model
func (c *GeminiClient) GenerateVideo(ctx context.Context, prompt string, vertical bool) (string, error) {
	// Use Veo 3.1 for video generation with predictLongRunning endpoint
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/veo-3.1-generate-preview:predictLongRunning?key=%s", c.apiKey)

	aspectRatio := "16:9"
	if vertical {
		aspectRatio = "9:16"
	}

	reqBody := map[string]interface{}{
		"instances": []map[string]interface{}{
			{
				"prompt": prompt,
			},
		},
		"parameters": map[string]interface{}{
			"aspectRatio": aspectRatio,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != 200 {
		var errResp struct {
			Error *apiError `json:"error,omitempty"`
		}
		json.Unmarshal(body, &errResp)
		if errResp.Error != nil {
			return "", fmt.Errorf("API error: %s (code %d)", errResp.Error.Message, errResp.Error.Code)
		}
		return "", fmt.Errorf("API error: status %d - %s", resp.StatusCode, string(body))
	}

	// Parse response - this returns an operation name for polling
	var opResp struct {
		Name  string    `json:"name"`
		Error *apiError `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &opResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if opResp.Error != nil {
		return "", fmt.Errorf("API error: %s (code %d)", opResp.Error.Message, opResp.Error.Code)
	}

	if opResp.Name == "" {
		return "", fmt.Errorf("no operation name returned. Response: %s", string(body))
	}

	// Poll for completion
	return c.pollVideoOperation(ctx, opResp.Name)
}

// pollVideoOperation polls for video generation completion
func (c *GeminiClient) pollVideoOperation(ctx context.Context, operationName string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s?key=%s", operationName, c.apiKey)

	for i := 0; i < 120; i++ { // Poll for up to 10 minutes
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}

		fmt.Printf("⏳ Polling for video completion (%d/120)...\n", i+1)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue // Retry on network error
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		// Try parsing with different response formats
		var opStatus struct {
			Done     bool `json:"done"`
			Response struct {
				// Veo 3.1 format
				GenerateVideoResponse struct {
					GeneratedSamples []struct {
						Video struct {
							URI string `json:"uri"`
						} `json:"video"`
					} `json:"generatedSamples"`
				} `json:"generateVideoResponse"`
				// Alternative format
				GeneratedVideos []struct {
					Video struct {
						URI string `json:"uri"`
					} `json:"video"`
				} `json:"generatedVideos"`
			} `json:"response"`
			Error *apiError `json:"error,omitempty"`
		}

		if err := json.Unmarshal(body, &opStatus); err != nil {
			fmt.Printf("Parse error: %v, body: %s\n", err, string(body)[:min(200, len(body))])
			continue
		}

		if opStatus.Error != nil {
			return "", fmt.Errorf("video generation failed: %s", opStatus.Error.Message)
		}

		if opStatus.Done {
			// Try generateVideoResponse format first
			if len(opStatus.Response.GenerateVideoResponse.GeneratedSamples) > 0 {
				uri := opStatus.Response.GenerateVideoResponse.GeneratedSamples[0].Video.URI
				if uri != "" {
					return uri, nil
				}
			}
			// Try generatedVideos format
			if len(opStatus.Response.GeneratedVideos) > 0 {
				uri := opStatus.Response.GeneratedVideos[0].Video.URI
				if uri != "" {
					return uri, nil
				}
			}
			return "", fmt.Errorf("video generation completed but no video URI found. Response: %s", string(body))
		}
	}

	return "", fmt.Errorf("video generation timed out after 10 minutes")
}

// GenerateVideoFromImages generates a video from multiple images
func (c *GeminiClient) GenerateVideoFromImages(ctx context.Context, imagePaths []string, prompt string) (string, error) {
	// Use Veo 3.1 with first frame (and optionally last frame)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/veo-3.1-generate-preview:predictLongRunning?key=%s", c.apiKey)

	if len(imagePaths) == 0 {
		return "", fmt.Errorf("no images provided")
	}

	// Read and encode first image
	firstImageData, err := os.ReadFile(imagePaths[0])
	if err != nil {
		return "", fmt.Errorf("failed to read first image %s: %w", imagePaths[0], err)
	}
	firstMimeType := getMimeType(imagePaths[0])
	firstEncoded := base64.StdEncoding.EncodeToString(firstImageData)

	// Build instance with first image
	instance := map[string]interface{}{
		"prompt": prompt,
		"image": map[string]interface{}{
			"bytesBase64Encoded": firstEncoded,
			"mimeType":           firstMimeType,
		},
	}

	// If we have a second image, use it as the last frame
	if len(imagePaths) >= 2 {
		lastImageData, err := os.ReadFile(imagePaths[1])
		if err != nil {
			return "", fmt.Errorf("failed to read last image %s: %w", imagePaths[1], err)
		}
		lastMimeType := getMimeType(imagePaths[1])
		lastEncoded := base64.StdEncoding.EncodeToString(lastImageData)

		instance["lastFrame"] = map[string]interface{}{
			"bytesBase64Encoded": lastEncoded,
			"mimeType":           lastMimeType,
		}
	}

	reqBody := map[string]interface{}{
		"instances": []map[string]interface{}{instance},
		"parameters": map[string]interface{}{
			"aspectRatio": "16:9",
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != 200 {
		var errResp struct {
			Error *apiError `json:"error,omitempty"`
		}
		json.Unmarshal(body, &errResp)
		if errResp.Error != nil {
			return "", fmt.Errorf("API error: %s (code %d)", errResp.Error.Message, errResp.Error.Code)
		}
		return "", fmt.Errorf("API error: status %d - %s", resp.StatusCode, string(body))
	}

	var opResp struct {
		Name  string    `json:"name"`
		Error *apiError `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &opResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if opResp.Error != nil {
		return "", fmt.Errorf("API error: %s (code %d)", opResp.Error.Message, opResp.Error.Code)
	}

	if opResp.Name == "" {
		return "", fmt.Errorf("no operation name returned. Response: %s", string(body))
	}

	return c.pollVideoOperation(ctx, opResp.Name)
}

// DownloadFile downloads a file from the Gemini API and saves it locally
func (c *GeminiClient) DownloadFile(ctx context.Context, fileURI string, outputPath string) (string, error) {
	// Add API key to the URI
	downloadURL := fileURI
	if strings.Contains(downloadURL, "?") {
		downloadURL += "&key=" + c.apiKey
	} else {
		downloadURL += "?key=" + c.apiKey
	}

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Copy response body to file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return outputPath, nil
}

// TextToSpeech converts text to speech using Gemini TTS
func (c *GeminiClient) TextToSpeech(ctx context.Context, text string, voice string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-preview-tts:generateContent?key=%s", c.apiKey)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": text},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"AUDIO"},
			"speechConfig": map[string]interface{}{
				"voiceConfig": map[string]interface{}{
					"prebuiltVoiceConfig": map[string]interface{}{
						"voiceName": voice,
					},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Retry up to 3 times for transient errors
	var body []byte
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}

		if resp.StatusCode == 500 || resp.StatusCode == 503 {
			lastErr = fmt.Errorf("TTS API error: status %d (attempt %d/3)", resp.StatusCode, attempt)
			fmt.Printf("⚠️ TTS API returned %d, retrying in %d seconds...\n", resp.StatusCode, attempt*2)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}

		if resp.StatusCode != 200 {
			return "", fmt.Errorf("TTS API error: status %d - %s", resp.StatusCode, string(body))
		}

		// Success
		lastErr = nil
		break
	}

	if lastErr != nil {
		return "", lastErr
	}

	// Parse response to get audio data
	var ttsResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &ttsResp); err != nil {
		return "", fmt.Errorf("failed to parse TTS response: %w", err)
	}

	if len(ttsResp.Candidates) == 0 || len(ttsResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no audio data in response")
	}

	// Decode base64 audio data - this is raw PCM (s16le, 24kHz, mono)
	pcmData, err := base64.StdEncoding.DecodeString(ttsResp.Candidates[0].Content.Parts[0].InlineData.Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode audio data: %w", err)
	}

	// Save raw PCM to temp file
	pcmPath := fmt.Sprintf("tts_raw_%d.pcm", time.Now().UnixNano())
	if err := os.WriteFile(pcmPath, pcmData, 0644); err != nil {
		return "", fmt.Errorf("failed to write PCM file: %w", err)
	}

	// Use ffmpeg to convert raw PCM to WAV
	outputPath := fmt.Sprintf("tts_output_%d.wav", time.Now().UnixNano())
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "s16le", // signed 16-bit little-endian
		"-ar", "24000", // 24kHz sample rate
		"-ac", "1", // mono
		"-i", pcmPath, // input raw PCM
		outputPath, // output WAV
	)

	output, err := cmd.CombinedOutput()
	os.Remove(pcmPath) // Clean up temp file

	if err != nil {
		return "", fmt.Errorf("ffmpeg conversion failed: %v\nOutput: %s", err, string(output))
	}

	return outputPath, nil
}

// writePCMToWav writes raw PCM data to a WAV file with proper header
func writePCMToWav(filename string, pcmData []byte, sampleRate, numChannels, bitsPerSample int) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Calculate sizes
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := len(pcmData)

	// RIFF header
	file.Write([]byte("RIFF"))
	binary.Write(file, binary.LittleEndian, uint32(36+dataSize)) // file size - 8
	file.Write([]byte("WAVE"))

	// fmt subchunk
	file.Write([]byte("fmt "))
	binary.Write(file, binary.LittleEndian, uint32(16))            // subchunk size
	binary.Write(file, binary.LittleEndian, uint16(1))             // audio format (1 = PCM)
	binary.Write(file, binary.LittleEndian, uint16(numChannels))   // num channels
	binary.Write(file, binary.LittleEndian, uint32(sampleRate))    // sample rate
	binary.Write(file, binary.LittleEndian, uint32(byteRate))      // byte rate
	binary.Write(file, binary.LittleEndian, uint16(blockAlign))    // block align
	binary.Write(file, binary.LittleEndian, uint16(bitsPerSample)) // bits per sample

	// data subchunk
	file.Write([]byte("data"))
	binary.Write(file, binary.LittleEndian, uint32(dataSize))
	file.Write(pcmData)

	return nil
}
