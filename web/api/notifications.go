package api

import (
	"encoding/json"
	"net/http"

	"github.com/cyntr-dev/cyntr/modules/notify"
)

func (s *Server) handleNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		Respond(w, 200, []string{})
		return
	}
	Respond(w, 200, s.notifier.ChannelNames())
}

func (s *Server) handleNotificationTest(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "notifier not initialized")
		return
	}
	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Severity string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	err := s.notifier.Send(r.Context(), notify.Notification{
		Title: body.Title, Message: body.Message, Severity: body.Severity,
		Source: "api-test",
	})
	if err != nil {
		RespondError(w, 500, "SEND_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "sent"})
}
