package main

import (
	"net/url"
	"testing"
)

func TestURLEncode(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"hello world", "hello+world"},
		{"a&b", "a%26b"},
		{"x=y", "x%3Dy"},
		{"no spaces", "no+spaces"},
		{"", ""},
		{"100%", "100%25"},
	}
	for _, tt := range tests {
		result := url.QueryEscape(tt.input)
		if result != tt.expected {
			t.Errorf("url.QueryEscape(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
