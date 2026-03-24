package api

import "testing"

func TestGetEnvOrDefault(t *testing.T) {
	if getEnvOrDefault("NONEXISTENT_VAR_XYZ", "fallback") != "fallback" {
		t.Fatal("expected fallback")
	}
}
