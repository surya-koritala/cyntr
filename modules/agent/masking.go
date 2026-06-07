package agent

import (
	"regexp"
	"sync"
)

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

// customPatterns holds user-configured secret patterns loaded at runtime.
// It is guarded by customPatternsMu because LoadSecretPatterns can replace it
// concurrently with MaskSecrets readers.
var (
	customPatternsMu sync.RWMutex
	customPatterns   []*regexp.Regexp
)

// LoadSecretPatterns compiles and registers additional secret patterns from configuration.
// Invalid regex patterns are silently skipped.
func LoadSecretPatterns(patterns []string) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	customPatternsMu.Lock()
	customPatterns = compiled
	customPatternsMu.Unlock()
}

// MaskSecrets replaces detected secret patterns in text with ***REDACTED***.
func MaskSecrets(text string) string {
	for _, pat := range secretPatterns {
		text = pat.ReplaceAllString(text, "***REDACTED***")
	}
	customPatternsMu.RLock()
	custom := customPatterns
	customPatternsMu.RUnlock()
	for _, pat := range custom {
		text = pat.ReplaceAllString(text, "***REDACTED***")
	}
	return text
}
