package providers

import "strings"

// OpenAICompatible is a generic provider for any OpenAI-compatible chat
// completions endpoint (NovitaAI, z.ai/GLM, Kimi/Moonshot, MiniMax, NVIDIA
// NIM, vLLM, LM Studio, …). It reuses the full OpenAI client and only
// overrides the provider name, so many such endpoints can be registered side
// by side under distinct names without per-vendor code.
type OpenAICompatible struct {
	*OpenAI
	name string
}

// NewOpenAICompatible builds a provider named `name` that talks to an
// OpenAI-compatible API at baseURL using model and apiKey.
func NewOpenAICompatible(name, apiKey, model, baseURL string) *OpenAICompatible {
	if name == "" {
		name = "openai-compatible"
	}
	return &OpenAICompatible{
		OpenAI: NewOpenAI(apiKey, model, baseURL),
		name:   strings.TrimSpace(name),
	}
}

// Name returns the configured provider name (overriding the embedded "gpt").
func (o *OpenAICompatible) Name() string { return o.name }
