package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// maxWebhookBody bounds the payload a webhook caller may send. Webhook bodies
// are forwarded to workflows/agents, so an unbounded read is a memory-DoS.
const maxWebhookBody = 1 << 20 // 1 MiB

// webhookSecret returns the shared secret used to authenticate inbound
// webhooks, read from CYNTR_WEBHOOK_SECRET. When unset, webhook endpoints are
// disabled (fail closed) — they perform privileged, fully parameterized
// workflow/agent actions, so they must never run unauthenticated.
func webhookSecret() string { return os.Getenv("CYNTR_WEBHOOK_SECRET") }

// verifyWebhookSignature authenticates an inbound webhook. It requires a
// configured secret and a valid HMAC-SHA256 signature of the raw body in the
// X-Cyntr-Signature header (hex-encoded). Returns false (and writes the
// response) when the webhook cannot be authenticated. The comparison is
// constant-time.
func verifyWebhookSignature(w http.ResponseWriter, body []byte, r *http.Request) bool {
	secret := webhookSecret()
	if secret == "" {
		RespondError(w, 503, "WEBHOOKS_DISABLED", "webhooks are disabled: set CYNTR_WEBHOOK_SECRET to enable")
		return false
	}
	sig := r.Header.Get("X-Cyntr-Signature")
	if sig == "" {
		RespondError(w, 401, "UNAUTHORIZED", "missing X-Cyntr-Signature")
		return false
	}
	provided, err := hex.DecodeString(sig)
	if err != nil {
		RespondError(w, 401, "UNAUTHORIZED", "invalid signature encoding")
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(provided, expected) {
		RespondError(w, 401, "UNAUTHORIZED", "invalid webhook signature")
		return false
	}
	return true
}

func (s *Server) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	wfID := r.PathValue("workflow_id")

	// Read the webhook payload (size-bounded) before authenticating, since the
	// signature is computed over the raw body.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		RespondError(w, 400, "READ_ERROR", err.Error())
		return
	}
	if !verifyWebhookSignature(w, body, r) {
		return
	}

	var payload map[string]any
	json.Unmarshal(body, &payload)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "webhook", Target: "workflow", Topic: "workflow.run",
		Payload: map[string]string{"workflow_id": wfID},
	})
	if err != nil {
		RespondError(w, 500, "TRIGGER_FAILED", err.Error())
		return
	}

	Respond(w, 200, map[string]any{
		"status":      "triggered",
		"workflow_id": wfID,
		"run_id":      resp.Payload,
		"payload":     payload,
	})
}

func (s *Server) handleWebhookAgent(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	agentName := r.PathValue("agent")

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		RespondError(w, 400, "READ_ERROR", err.Error())
		return
	}
	if !verifyWebhookSignature(w, body, r) {
		return
	}
	// If the caller was authenticated with a tenant-bound identity, the path
	// tenant must match it — a webhook secret holder cannot drive arbitrary
	// tenants beyond what their credential authorizes.
	if !enforceTenant(w, r, tenant) {
		return
	}

	// Try to extract message from JSON body
	var payload struct {
		Message string `json:"message"`
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	json.Unmarshal(body, &payload)

	message := payload.Message
	if message == "" {
		message = payload.Text
	}
	if message == "" {
		message = payload.Content
	}
	if message == "" {
		message = string(body)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "webhook", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tenant,
			User:    "webhook:" + r.RemoteAddr,
			Message: message,
		},
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

	Respond(w, 200, map[string]any{
		"agent":      chatResp.Agent,
		"content":    chatResp.Content,
		"tools_used": chatResp.ToolsUsed,
	})
}
