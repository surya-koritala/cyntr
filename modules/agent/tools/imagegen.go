package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type ImageGenTool struct {
	client *http.Client
	apiURL string
}

func NewImageGenTool() *ImageGenTool {
	return &ImageGenTool{
		client: &http.Client{Timeout: 60 * time.Second},
		apiURL: "https://api.openai.com/v1/images/generations",
	}
}

func (t *ImageGenTool) SetAPIURL(url string) { t.apiURL = url }

func (t *ImageGenTool) Name() string { return "generate_image" }
func (t *ImageGenTool) Description() string {
	return "Generate an image using OpenAI DALL-E API. Returns the image URL and revised prompt."
}
func (t *ImageGenTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"prompt":  {Type: "string", Description: "Description of the image to generate", Required: true},
		"api_key": {Type: "string", Description: "OpenAI API key", Required: true},
		"size":    {Type: "string", Description: "Image size: 256x256, 512x512, 1024x1024, 1792x1024, 1024x1792 (default 1024x1024)", Required: false},
		"model":   {Type: "string", Description: "DALL-E model: dall-e-2 or dall-e-3 (default dall-e-3)", Required: false},
	}
}

func (t *ImageGenTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	prompt := input["prompt"]
	apiKey := input["api_key"]
	if prompt == "" || apiKey == "" {
		return "", fmt.Errorf("prompt and api_key are required")
	}

	model := input["model"]
	if model == "" {
		model = "dall-e-3"
	}

	size := input["size"]
	if size == "" {
		size = "1024x1024"
	}

	reqBody := map[string]any{
		"model":  model,
		"prompt": prompt,
		"n":      1,
		"size":   size,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("image generation API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Data []struct {
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("no image generated")
	}

	img := result.Data[0]
	output := fmt.Sprintf("Image URL: %s\n\nRevised Prompt: %s", img.URL, img.RevisedPrompt)
	return output, nil
}
