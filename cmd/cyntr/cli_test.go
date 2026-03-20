package main

import "testing"

func TestAPIURL(t *testing.T) {
	url := apiURL()
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
}
