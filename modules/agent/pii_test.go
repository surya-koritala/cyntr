package agent

import (
	"strings"
	"testing"
)

func TestDetectPIISSN(t *testing.T) {
	found := DetectPII("My SSN is 123-45-6789")
	if len(found) == 0 || found[0] != "SSN" {
		t.Fatalf("expected SSN, got %v", found)
	}
}

func TestDetectPIICreditCard(t *testing.T) {
	found := DetectPII("Card: 4111 1111 1111 1111")
	if len(found) == 0 {
		t.Fatal("expected credit card detection")
	}
}

func TestDetectPIIEmail(t *testing.T) {
	found := DetectPII("Contact alice@example.com for details")
	hasEmail := false
	for _, f := range found {
		if f == "Email" {
			hasEmail = true
		}
	}
	if !hasEmail {
		t.Fatalf("expected Email, got %v", found)
	}
}

func TestDetectPIIPhone(t *testing.T) {
	found := DetectPII("Call me at (555) 123-4567")
	hasPhone := false
	for _, f := range found {
		if f == "Phone" {
			hasPhone = true
		}
	}
	if !hasPhone {
		t.Fatalf("expected Phone, got %v", found)
	}
}

func TestDetectPIINone(t *testing.T) {
	found := DetectPII("The weather is sunny today")
	if len(found) != 0 {
		t.Fatalf("expected no PII, got %v", found)
	}
}

func TestRedactPIISSN(t *testing.T) {
	result := RedactPII("SSN: 123-45-6789")
	if strings.Contains(result, "123-45-6789") {
		t.Fatal("SSN not redacted")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Fatal("missing redaction marker")
	}
}

func TestRedactPIIEmail(t *testing.T) {
	result := RedactPII("Email: test@example.com")
	if strings.Contains(result, "test@example.com") {
		t.Fatal("email not redacted")
	}
}

func TestRedactPIIMultiple(t *testing.T) {
	text := "SSN 123-45-6789 email test@example.com phone (555) 123-4567"
	result := RedactPII(text)
	if strings.Contains(result, "123-45-6789") || strings.Contains(result, "test@example.com") {
		t.Fatal("not all PII redacted")
	}
}

func TestRedactPIIPreservesNormal(t *testing.T) {
	text := "The server is running at version 2.5"
	result := RedactPII(text)
	if result != text {
		t.Fatalf("normal text was modified: %q", result)
	}
}
