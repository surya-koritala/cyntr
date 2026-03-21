package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Envelope is the standard API response wrapper.
type Envelope struct {
	Data  any       `json:"data"`
	Meta  Meta      `json:"meta"`
	Error *APIError `json:"error"`
}

// Meta contains request metadata.
type Meta struct {
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

// APIError is a structured error response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Respond writes a success response with the standard envelope.
func Respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Envelope{
		Data:  data,
		Meta:  newMeta(),
		Error: nil,
	})
}

// RespondError writes an error response with the standard envelope.
func RespondError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Envelope{
		Data: nil,
		Meta: newMeta(),
		Error: &APIError{
			Code:    code,
			Message: message,
		},
	})
}

func newMeta() Meta {
	buf := make([]byte, 8)
	rand.Read(buf)
	return Meta{
		RequestID: hex.EncodeToString(buf),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// traceID extracts a trace ID from the incoming request's X-Request-ID header.
// If the header is absent, it generates one from the current nanosecond timestamp.
// This value is propagated through the IPC bus so that every message in the
// request chain can be correlated back to the originating HTTP request.
func traceID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
