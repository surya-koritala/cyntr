package agent

import (
	"strings"
	"testing"
)

func TestMaskSecretsMultiplePatterns(t *testing.T) {
	text := "AWS key: AKIAIOSFODNN7EXAMPLE and token: xoxb-123-456-abcdefghijklmnopqrstuv"
	result := MaskSecrets(text)
	if strings.Contains(result, "AKIA") || strings.Contains(result, "xoxb-") {
		t.Fatalf("expected both masked, got: %s", result)
	}
}

func TestLoadCustomSecretPatterns(t *testing.T) {
	LoadSecretPatterns([]string{`CUSTOM_[A-Z]{10}`})
	text := "My key is CUSTOM_ABCDEFGHIJ"
	result := MaskSecrets(text)
	if strings.Contains(result, "CUSTOM_") {
		t.Fatalf("custom pattern not masked: %s", result)
	}
	// Reset
	LoadSecretPatterns(nil)
}

func TestMaskSecretsEmptyInput(t *testing.T) {
	if MaskSecrets("") != "" {
		t.Fatal("expected empty output for empty input")
	}
}
