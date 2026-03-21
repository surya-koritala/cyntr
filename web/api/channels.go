package api

import (
	"context"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func (s *Server) handleChannelList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "channel", Topic: "channel.details",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}
