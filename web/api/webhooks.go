package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func (s *Server) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	wfID := r.PathValue("workflow_id")

	// Read the webhook payload and store it for the workflow
	body, _ := io.ReadAll(r.Body)
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
