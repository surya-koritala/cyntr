package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type TranscribeTool struct {
	client *http.Client
	apiURL string
}

func NewTranscribeTool() *TranscribeTool {
	return &TranscribeTool{
		client: &http.Client{Timeout: 60 * time.Second},
		apiURL: "https://api.openai.com/v1/audio/transcriptions",
	}
}

func (t *TranscribeTool) SetAPIURL(url string) { t.apiURL = url }

func (t *TranscribeTool) Name() string { return "transcribe_audio" }
func (t *TranscribeTool) Description() string {
	return "Transcribe an audio file to text using speech-to-text. Supports mp3, wav, m4a, webm."
}
func (t *TranscribeTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"file_path": {Type: "string", Description: "Path to the audio file", Required: true},
		"api_key":   {Type: "string", Description: "OpenAI API key for Whisper", Required: true},
		"language":  {Type: "string", Description: "Language code (e.g., en, es, fr)", Required: false},
	}
}

func (t *TranscribeTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	filePath := input["file_path"]
	apiKey := input["api_key"]
	if filePath == "" || apiKey == "" {
		return "", fmt.Errorf("file_path and api_key are required")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filePath)
	if err != nil {
		return "", err
	}
	io.Copy(part, file)
	writer.WriteField("model", "whisper-1")
	if lang := input["language"]; lang != "" {
		writer.WriteField("language", lang)
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	return result.Text, nil
}
