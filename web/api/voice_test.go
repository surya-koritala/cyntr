package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
)

// mockSTT returns an httptest server emulating the Whisper transcription API.
func mockSTT(t *testing.T, text string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": text})
	}))
}

// mockTTS returns an httptest server emulating the speech synthesis API.
func mockTTS(t *testing.T, audio []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(audio)
	}))
}

func wireVoiceServer(t *testing.T, transcript string, replyAudio []byte) *Server {
	t.Helper()
	k, bus := setupKernel(t)
	srv := NewServer(bus, k)

	// Create the agent the voice endpoint will drive.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "voicebot", Tenant: "finance", Model: "mock",
			SystemPrompt: "You are a voice assistant.", Tools: []string{}, MaxTurns: 5,
		},
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	sttSrv := mockSTT(t, transcript)
	t.Cleanup(sttSrv.Close)
	ttsSrv := mockTTS(t, replyAudio)
	t.Cleanup(ttsSrv.Close)

	stt := agenttools.NewTranscribeTool()
	stt.SetAPIURL(sttSrv.URL)
	tts := agenttools.NewTextToSpeechTool()
	tts.SetAPIURL(ttsSrv.URL)
	srv.SetVoiceTools(stt, tts)
	return srv
}

func multipartAudio(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestVoiceRoundTrip_Multipart(t *testing.T) {
	replyAudio := []byte("SYNTHESIZED-REPLY-AUDIO")
	srv := wireVoiceServer(t, "hello agent", replyAudio)

	body, ct := multipartAudio(t, "file", "clip.mp3", []byte("fake-input-audio"))
	req := httptest.NewRequest("POST", "/api/v1/tenants/finance/agents/voicebot/voice", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-STT-Key", "stt-test-key")
	req.Header.Set("X-TTS-Key", "tts-test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.Bytes(); !bytes.Equal(got, replyAudio) {
		t.Fatalf("audio mismatch: got %q", string(got))
	}
	if w.Header().Get("X-Transcript") != "hello agent" {
		t.Fatalf("transcript header: %q", w.Header().Get("X-Transcript"))
	}
	// Mock provider replies "Test response"; that must be what got synthesized.
	if w.Header().Get("X-Reply-Text") != "Test response" {
		t.Fatalf("reply header: %q", w.Header().Get("X-Reply-Text"))
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type: %q", ct)
	}
}

func TestVoiceRoundTrip_RawBody(t *testing.T) {
	replyAudio := []byte("RAW-REPLY")
	srv := wireVoiceServer(t, "spoken question", replyAudio)

	req := httptest.NewRequest("POST", "/api/v1/tenants/finance/agents/voicebot/voice",
		bytes.NewReader([]byte("raw-audio-bytes")))
	req.Header.Set("Content-Type", "audio/wav")
	req.Header.Set("X-STT-Key", "stt-test-key")
	req.Header.Set("X-TTS-Key", "tts-test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), replyAudio) {
		t.Fatalf("audio mismatch")
	}
}

func TestVoiceEmptyUpload(t *testing.T) {
	srv := wireVoiceServer(t, "ignored", []byte("x"))
	req := httptest.NewRequest("POST", "/api/v1/tenants/finance/agents/voicebot/voice",
		bytes.NewReader(nil))
	req.Header.Set("Content-Type", "audio/mpeg")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for empty upload, got %d", w.Code)
	}
}

func TestVoiceEmptyTranscript(t *testing.T) {
	// STT returns nothing -> 422 before any agent turn.
	srv := wireVoiceServer(t, "", []byte("x"))
	req := httptest.NewRequest("POST", "/api/v1/tenants/finance/agents/voicebot/voice",
		bytes.NewReader([]byte("audio")))
	req.Header.Set("Content-Type", "audio/mpeg")
	req.Header.Set("X-STT-Key", "stt-test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 422 {
		t.Fatalf("expected 422 for empty transcript, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestVoiceHeaderSanitization(t *testing.T) {
	// A transcript with CRLF must not leak into response headers.
	srv := wireVoiceServer(t, "line1\r\nInjected: evil", []byte("a"))
	req := httptest.NewRequest("POST", "/api/v1/tenants/finance/agents/voicebot/voice",
		bytes.NewReader([]byte("audio")))
	req.Header.Set("Content-Type", "audio/mpeg")
	req.Header.Set("X-STT-Key", "stt-test-key")
	req.Header.Set("X-TTS-Key", "tts-test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	h := w.Header().Get("X-Transcript")
	if bytes.ContainsAny([]byte(h), "\r\n") {
		t.Fatalf("header contains CRLF: %q", h)
	}
	if w.Header().Get("Injected") != "" {
		t.Fatal("header injection succeeded")
	}
}
