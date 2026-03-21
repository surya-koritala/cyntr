package agent

import (
	"strings"
	"testing"
)

func TestMaskAWSKey(t *testing.T) {
	text := "Found key: AKIAIOSFODNN7EXAMPLE in config"
	result := MaskSecrets(text)
	if strings.Contains(result, "AKIA") {
		t.Fatalf("AWS key not masked: %s", result)
	}
	if !strings.Contains(result, "***REDACTED***") {
		t.Fatalf("missing redaction marker: %s", result)
	}
}

func TestMaskSlackToken(t *testing.T) {
	text := "Token: xoxb-1234567890-abcdefghij-klmnopqrstuv"
	result := MaskSecrets(text)
	if strings.Contains(result, "xoxb-") {
		t.Fatalf("Slack token not masked: %s", result)
	}
}

func TestMaskGitHubToken(t *testing.T) {
	text := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef"
	result := MaskSecrets(text)
	if strings.Contains(result, "ghp_") {
		t.Fatalf("GitHub token not masked: %s", result)
	}
}

func TestMaskJWT(t *testing.T) {
	text := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	result := MaskSecrets(text)
	if strings.Contains(result, "eyJ") {
		t.Fatalf("JWT not masked: %s", result)
	}
}

func TestMaskCyntrKey(t *testing.T) {
	text := "key=cyntr_abcdef0123456789abcdef0123456789"
	result := MaskSecrets(text)
	if strings.Contains(result, "cyntr_") {
		t.Fatalf("Cyntr key not masked: %s", result)
	}
}

func TestMaskGenericPassword(t *testing.T) {
	text := "password=mysecretpass123"
	result := MaskSecrets(text)
	if strings.Contains(result, "mysecretpass") {
		t.Fatalf("password not masked: %s", result)
	}
}

func TestNoFalsePositive(t *testing.T) {
	text := "The weather is sunny today. Temperature is 72F."
	result := MaskSecrets(text)
	if result != text {
		t.Fatalf("false positive masking: %q -> %q", text, result)
	}
}

func TestMaskPreservesNormalText(t *testing.T) {
	text := "Hello world. AKIAIOSFODNN7EXAMPLE found. Goodbye."
	result := MaskSecrets(text)
	if !strings.Contains(result, "Hello world") || !strings.Contains(result, "Goodbye") {
		t.Fatalf("normal text was damaged: %s", result)
	}
}
