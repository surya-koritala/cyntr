package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockTTSServer returns an httptest server that echoes fake audio bytes and
// asserts the request shape. The asserted text/voice/model are returned via the
// passed-in pointer so a test can verify what reached the provider.
func mockTTSServer(t *testing.T, audio []byte, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if captured != nil {
			*captured = body
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(200)
		w.Write(audio)
	}))
}

func TestTextToSpeechToolName(t *testing.T) {
	if NewTextToSpeechTool().Name() != "text_to_speech" {
		t.Fatal("unexpected name")
	}
}

func TestTextToSpeechToolExecute(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]string
		wantVoice string
		wantModel string
	}{
		{
			name:      "defaults",
			input:     map[string]string{"text": "hello world", "api_key": "test-key"},
			wantVoice: "alloy",
			wantModel: "tts-1",
		},
		{
			name:      "explicit voice and model",
			input:     map[string]string{"text": "hi", "api_key": "test-key", "voice": "nova", "model": "tts-1-hd"},
			wantVoice: "nova",
			wantModel: "tts-1-hd",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			srv := mockTTSServer(t, []byte("FAKE-AUDIO-BYTES"), &captured)
			defer srv.Close()

			dir := t.TempDir()
			tool := NewTextToSpeechTool()
			tool.SetAPIURL(srv.URL)
			tool.SetOutputDir(dir)

			out, err := tool.Execute(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if captured["voice"] != tc.wantVoice {
				t.Fatalf("voice: got %v want %s", captured["voice"], tc.wantVoice)
			}
			if captured["model"] != tc.wantModel {
				t.Fatalf("model: got %v want %s", captured["model"], tc.wantModel)
			}

			// Result must reference a real file containing the audio bytes.
			fields := strings.Fields(out)
			if len(fields) < 4 || fields[0] != "Audio" {
				t.Fatalf("unexpected output: %q", out)
			}
			path := fields[3]
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if string(data) != "FAKE-AUDIO-BYTES" {
				t.Fatalf("audio mismatch: %q", string(data))
			}
		})
	}
}

func TestTextToSpeechToolExplicitOutPath(t *testing.T) {
	srv := mockTTSServer(t, []byte("AAA"), nil)
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "nested", "speech.mp3")

	tool := NewTextToSpeechTool()
	tool.SetAPIURL(srv.URL)

	if _, err := tool.Execute(context.Background(), map[string]string{
		"text": "hello", "api_key": "k", "out_path": out,
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if data, err := os.ReadFile(out); err != nil || string(data) != "AAA" {
		t.Fatalf("expected audio at %s, err=%v", out, err)
	}
}

func TestTextToSpeechToolMissingText(t *testing.T) {
	tool := NewTextToSpeechTool()
	if _, err := tool.Execute(context.Background(), map[string]string{"api_key": "k"}); err == nil {
		t.Fatal("expected error for missing text")
	}
}

func TestTextToSpeechToolAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()

	tool := NewTextToSpeechTool()
	tool.SetAPIURL(srv.URL)
	if _, err := tool.Execute(context.Background(), map[string]string{"text": "hi", "api_key": "bad"}); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestTextToSpeechToolEmptyAudio(t *testing.T) {
	srv := mockTTSServer(t, []byte{}, nil)
	defer srv.Close()

	tool := NewTextToSpeechTool()
	tool.SetAPIURL(srv.URL)
	if _, err := tool.Execute(context.Background(), map[string]string{"text": "hi", "api_key": "k"}); err == nil {
		t.Fatal("expected error on empty audio")
	}
}
