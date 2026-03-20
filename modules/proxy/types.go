package proxy

// Intent represents the semantic meaning extracted from an HTTP request.
type Intent struct {
	Action   string // "model_call", "tool_call", "unknown"
	Provider string // "anthropic", "openai", ""
	Model    string // model name if detected
	Tool     string // tool name if detected
}

// ExternalAgent represents a registered external agent backend.
type ExternalAgent struct {
	Name     string `yaml:"name"`
	Tenant   string `yaml:"tenant"`
	Type     string `yaml:"type"`     // "openclaw", "langchain", etc.
	Endpoint string `yaml:"endpoint"` // upstream URL
}

// Key returns the unique identifier for this external agent.
func (ea ExternalAgent) Key() string {
	return ea.Tenant + "/" + ea.Name
}
