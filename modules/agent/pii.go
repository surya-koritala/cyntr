package agent

import "regexp"

var piiPatterns = []*regexp.Regexp{
	// Social Security Numbers (US)
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	// Credit card numbers (basic patterns)
	regexp.MustCompile(`\b(?:\d{4}[- ]?){3}\d{4}\b`),
	// Email addresses
	regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	// Phone numbers (US formats)
	regexp.MustCompile(`\b(?:\+1[- ]?)?\(?\d{3}\)?[- ]?\d{3}[- ]?\d{4}\b`),
	// IP addresses
	regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
	// Date of birth patterns (MM/DD/YYYY, MM-DD-YYYY)
	regexp.MustCompile(`\b(?:0[1-9]|1[0-2])[/\-](?:0[1-9]|[12]\d|3[01])[/\-](?:19|20)\d{2}\b`),
}

// DetectPII scans text and returns a list of PII types found.
func DetectPII(text string) []string {
	piiNames := []string{"SSN", "Credit Card", "Email", "Phone", "IP Address", "Date of Birth"}
	var found []string
	for i, pat := range piiPatterns {
		if pat.MatchString(text) {
			found = append(found, piiNames[i])
		}
	}
	return found
}

// RedactPII replaces detected PII with [REDACTED] markers.
func RedactPII(text string) string {
	for _, pat := range piiPatterns {
		text = pat.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}
