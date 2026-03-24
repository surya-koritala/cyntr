package main

import "testing"

func TestURLEncode(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"hello world", "hello+world"},
		{"a&b", "a%26b"},
		{"x=y", "x%3Dy"},
		{"no spaces", "no+spaces"},
		{"", ""},
	}
	for _, tt := range tests {
		result := urlEncode(tt.input)
		if result != tt.expected {
			t.Errorf("urlEncode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
