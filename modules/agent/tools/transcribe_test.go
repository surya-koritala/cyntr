package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscribeToolName(t *testing.T) {
	if NewTranscribeTool().Name() != "transcribe_audio" {
		t.Fatal()
	}
}

func TestTranscribeToolExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing auth")
		}
		if r.Method != "POST" {
			t.Fatal("expected POST")
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "Hello, this is a transcribed message."})
	}))
	defer server.Close()

	// Create a dummy audio file
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "test.mp3")
	os.WriteFile(audioPath, []byte("fake audio data"), 0644)

	tool := NewTranscribeTool()
	tool.SetAPIURL(server.URL)

	result, err := tool.Execute(context.Background(), map[string]string{
		"file_path": audioPath, "api_key": "test-key",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "transcribed message") {
		t.Fatalf("got %q", result)
	}
}

func TestTranscribeToolMissingFile(t *testing.T) {
	tool := NewTranscribeTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"file_path": "/nonexistent/audio.mp3", "api_key": "key",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTranscribeToolMissingParams(t *testing.T) {
	tool := NewTranscribeTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTranscribeToolAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":"invalid key"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	audioPath := filepath.Join(dir, "test.mp3")
	os.WriteFile(audioPath, []byte("fake"), 0644)

	tool := NewTranscribeTool()
	tool.SetAPIURL(server.URL)
	_, err := tool.Execute(context.Background(), map[string]string{
		"file_path": audioPath, "api_key": "bad-key",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
