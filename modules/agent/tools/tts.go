package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// TextToSpeechTool synthesizes speech audio from text (B8 — the server-side
// complement to TranscribeTool). It mirrors the STT pattern: a thin provider
// call with a configurable endpoint so tests can point it at an httptest
// server. The provider is selectable (OpenAI-style by default) and routes
// through the bundled tool gateway (D19, CapTTS) when no per-vendor key is set.
//
// The synthesized audio is written to a file and the path is returned, so the
// bytes never land in tool-result transcripts or logs (the API key likewise is
// only ever sent as a bearer header, never logged).
type TextToSpeechTool struct {
	client *http.Client
	apiURL string
	// outDir is where synthesized audio files are written. Empty => os.TempDir.
	outDir string
}

// NewTextToSpeechTool builds the tool. With no explicit OPENAI key it routes
// TTS through the configured tool gateway; otherwise it talks to OpenAI's
// speech endpoint directly. Behaviour with neither is the vendor default
// (the api_key must then be supplied per-call, exactly like TranscribeTool).
func NewTextToSpeechTool() *TextToSpeechTool {
	vendorURL := "https://api.openai.com/v1/audio/speech"
	url, _, _ := ToolGatewayFromEnv().Endpoint(CapTTS, vendorURL, os.Getenv("OPENAI_API_KEY"))
	return &TextToSpeechTool{
		client: &http.Client{Timeout: 60 * time.Second},
		apiURL: url,
	}
}

// SetAPIURL overrides the synthesis endpoint (used by tests / custom gateways).
func (t *TextToSpeechTool) SetAPIURL(url string) { t.apiURL = url }

// SetOutputDir overrides where audio files are written.
func (t *TextToSpeechTool) SetOutputDir(dir string) { t.outDir = dir }

func (t *TextToSpeechTool) Name() string { return "text_to_speech" }

func (t *TextToSpeechTool) Description() string {
	return "Synthesize speech audio from text using text-to-speech. Writes an audio file and returns its path."
}

func (t *TextToSpeechTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"text":     {Type: "string", Description: "Text to synthesize into speech", Required: true},
		"api_key":  {Type: "string", Description: "Provider API key (omit to use the configured tool gateway)", Required: false},
		"voice":    {Type: "string", Description: "Voice name (provider-specific, e.g. alloy)", Required: false},
		"model":    {Type: "string", Description: "TTS model (default tts-1)", Required: false},
		"format":   {Type: "string", Description: "Audio format: mp3, wav, opus, aac, flac (default mp3)", Required: false},
		"out_path": {Type: "string", Description: "Optional output file path; a temp file is created if omitted", Required: false},
	}
}

// Synthesize performs the provider call and returns the raw audio bytes plus
// the chosen format. It is exported so the voice round-trip endpoint can reuse
// the exact same provider path without going through file I/O.
func (t *TextToSpeechTool) Synthesize(ctx context.Context, text, apiKey, voice, model, format string) ([]byte, string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, "", fmt.Errorf("text is required")
	}
	if voice == "" {
		voice = "alloy"
	}
	if model == "" {
		model = "tts-1"
	}
	if format == "" {
		format = "mp3"
	}

	reqBody := map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": format,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("tts API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Cap the error body so a provider error page can't bloat logs, and
		// never echo the request (which contains the synthesized text).
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("tts API error %d: %s", resp.StatusCode, string(b))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read audio: %w", err)
	}
	if len(audio) == 0 {
		return nil, "", fmt.Errorf("provider returned no audio")
	}
	return audio, format, nil
}

func (t *TextToSpeechTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	audio, format, err := t.Synthesize(ctx, input["text"], input["api_key"], input["voice"], input["model"], input["format"])
	if err != nil {
		return "", err
	}

	outPath := input["out_path"]
	if outPath == "" {
		dir := t.outDir
		if dir == "" {
			dir = os.TempDir()
		}
		f, err := os.CreateTemp(dir, "cyntr-tts-*."+format)
		if err != nil {
			return "", fmt.Errorf("create output file: %w", err)
		}
		outPath = f.Name()
		f.Close()
	} else if d := filepath.Dir(outPath); d != "" {
		os.MkdirAll(d, 0o755)
	}

	if err := os.WriteFile(outPath, audio, 0o644); err != nil {
		return "", fmt.Errorf("write audio: %w", err)
	}

	return fmt.Sprintf("Audio written to %s (%d bytes, %s)", outPath, len(audio), format), nil
}
