package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
)

// Voice round-trip endpoint (B8).
//
// POST /api/v1/tenants/{tid}/agents/{name}/voice takes an audio clip and runs
// the full conversational loop server-side:
//
//	audio -> transcribe (STT) -> agent turn -> text_to_speech (TTS) -> audio
//
// STT and TTS each go through their existing provider tools, so the provider is
// selectable per the same precedence the tools use: an explicit per-call key
// (X-STT-Key / X-TTS-Key header) wins, otherwise the configured tool gateway
// (CYNTR_TOOL_GATEWAY_* / CapTTS) is used, otherwise the vendor default. The
// agent turn itself routes through the IPC bus, so policy + audit + quota are
// enforced by the runtime exactly as for a text chat.
//
// The reply audio is streamed back as the response body; the plain-text
// transcript and reply are surfaced via response headers so a caller can show
// them without a second request. Keys arrive only in headers and are never
// logged or echoed.

const maxVoiceUpload = 25 << 20 // 25 MiB, matching common Whisper limits.

// sttTool / ttsTool let tests inject mock-backed tools (pointing SetAPIURL at
// an httptest server). In production they're lazily built from env on first
// use, exactly like the registered copies.
type voiceTools struct {
	stt *agenttools.TranscribeTool
	tts *agenttools.TextToSpeechTool
}

// SetVoiceTools overrides the STT/TTS tools used by the voice endpoint. Either
// argument may be nil to keep the default. Tests use this to inject mocks.
func (s *Server) SetVoiceTools(stt *agenttools.TranscribeTool, tts *agenttools.TextToSpeechTool) {
	if s.voice == nil {
		s.voice = &voiceTools{}
	}
	if stt != nil {
		s.voice.stt = stt
	}
	if tts != nil {
		s.voice.tts = tts
	}
}

func (s *Server) sttTool() *agenttools.TranscribeTool {
	if s.voice != nil && s.voice.stt != nil {
		return s.voice.stt
	}
	return agenttools.NewTranscribeTool()
}

func (s *Server) ttsTool() *agenttools.TextToSpeechTool {
	if s.voice != nil && s.voice.tts != nil {
		return s.voice.tts
	}
	return agenttools.NewTextToSpeechTool()
}

func (s *Server) handleAgentVoice(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	agentName := r.PathValue("name")
	if tid == "" || agentName == "" {
		RespondError(w, 400, "INVALID_REQUEST", "tenant and agent are required")
		return
	}

	// 1) Read the uploaded audio (multipart "file" field or raw body).
	audio, ext, err := readVoiceUpload(r)
	if err != nil {
		RespondError(w, 400, "READ_ERROR", err.Error())
		return
	}
	if len(audio) == 0 {
		RespondError(w, 400, "INVALID_REQUEST", "empty audio upload")
		return
	}

	tmpIn, err := os.CreateTemp("", "cyntr-voice-in-*"+ext)
	if err != nil {
		RespondError(w, 500, "INTERNAL", "could not buffer audio")
		return
	}
	tmpPath := tmpIn.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpIn.Write(audio); err != nil {
		tmpIn.Close()
		RespondError(w, 500, "INTERNAL", "could not buffer audio")
		return
	}
	tmpIn.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	// 2) Speech-to-text. Provider key from header (else gateway/vendor default).
	sttKey := r.Header.Get("X-STT-Key")
	transcript, err := s.sttTool().Execute(ctx, map[string]string{
		"file_path": tmpPath,
		"api_key":   sttKey,
		"language":  r.Header.Get("X-STT-Language"),
	})
	if err != nil {
		RespondError(w, 502, "STT_FAILED", "transcription failed")
		return
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		RespondError(w, 422, "STT_EMPTY", "no speech detected in audio")
		return
	}

	// 3) Agent turn over the IPC bus (policy/audit/quota enforced by runtime).
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tid,
			User:    apiUser(r.Header.Get("X-User")),
			Message: transcript,
		},
		TraceID: traceID(r),
	})
	if err != nil {
		RespondError(w, 500, "CHAT_FAILED", err.Error())
		return
	}
	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		RespondError(w, 500, "INTERNAL", "unexpected response type")
		return
	}
	reply := strings.TrimSpace(chatResp.Content)
	if reply == "" {
		RespondError(w, 502, "EMPTY_REPLY", "agent produced no text to synthesize")
		return
	}

	// 4) Text-to-speech of the reply. Provider key from header (else gateway).
	format := r.Header.Get("X-TTS-Format")
	if format == "" {
		format = "mp3"
	}
	out, format, err := s.ttsTool().Synthesize(ctx, reply,
		r.Header.Get("X-TTS-Key"), r.Header.Get("X-TTS-Voice"), r.Header.Get("X-TTS-Model"), format)
	if err != nil {
		RespondError(w, 502, "TTS_FAILED", "speech synthesis failed")
		return
	}

	// 5) Round-trip the audio back. Transcript + reply ride along as headers so
	// a client can render text without re-decoding the audio.
	w.Header().Set("Content-Type", audioMIME(format))
	w.Header().Set("X-Transcript", sanitizeHeader(transcript))
	w.Header().Set("X-Reply-Text", sanitizeHeader(reply))
	w.Header().Set("X-Agent", chatResp.Agent)
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// readVoiceUpload extracts audio bytes from a multipart "file" field if present,
// otherwise from the raw request body. It returns a file extension hint for the
// temp file (the STT provider sniffs by extension).
func readVoiceUpload(r *http.Request) ([]byte, string, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(maxVoiceUpload); err != nil {
			return nil, "", err
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxVoiceUpload))
		if err != nil {
			return nil, "", err
		}
		ext := filepath.Ext(hdr.Filename)
		if ext == "" {
			ext = ".mp3"
		}
		return data, ext, nil
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, maxVoiceUpload))
	if err != nil {
		return nil, "", err
	}
	return data, extFromContentType(ct), nil
}

func extFromContentType(ct string) string {
	switch {
	case strings.Contains(ct, "wav"):
		return ".wav"
	case strings.Contains(ct, "webm"):
		return ".webm"
	case strings.Contains(ct, "m4a"), strings.Contains(ct, "mp4"):
		return ".m4a"
	case strings.Contains(ct, "ogg"), strings.Contains(ct, "opus"):
		return ".ogg"
	default:
		return ".mp3"
	}
}

func audioMIME(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "opus", "ogg":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}

// sanitizeHeader strips CR/LF so transcript/reply text can't inject headers,
// and caps length to keep responses bounded.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 2048 {
		s = s[:2048]
	}
	return s
}
