package agent

import "regexp"

var secretPatterns = []*regexp.Regexp{
	// AWS Access Key IDs
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// AWS Secret Keys (40 char base64-like after known prefixes)
	regexp.MustCompile(`(?i)(aws_secret_access_key|secret_access_key|aws_secret)\s*[=:]\s*\S{20,45}`),
	// Slack tokens
	regexp.MustCompile(`xox[bpsa]-[0-9a-zA-Z\-]{20,}`),
	// GitHub tokens
	regexp.MustCompile(`gh[pso]_[A-Za-z0-9_]{20,}`),
	// JWT tokens
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
	// Cyntr API keys
	regexp.MustCompile(`cyntr_[a-f0-9]{32,}`),
	// Generic secret assignments
	regexp.MustCompile(`(?i)(password|secret|token|api_key|apikey|access_key|private_key)\s*[=:]\s*['"]?\S{8,}['"]?`),
}

// MaskSecrets replaces detected secret patterns in text with ***REDACTED***.
func MaskSecrets(text string) string {
	for _, pat := range secretPatterns {
		text = pat.ReplaceAllString(text, "***REDACTED***")
	}
	return text
}
