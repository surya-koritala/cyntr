package api

import (
	"context"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/curator"
)

// handleCuratorScores serves GET /api/v1/curator/scores. It
// proxies the request to the curator module via IPC and returns
// the aggregated SkillScore list. An optional ?skill_name=... query
// param filters to a single skill.
func (s *Server) handleCuratorScores(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	msg := ipc.Message{
		Source: "api", Target: curator.ModuleName, Topic: curator.TopicScores,
	}
	if name := r.URL.Query().Get("skill_name"); name != "" {
		msg.Payload = curator.ScoresFilter{SkillName: name}
	}

	resp, err := s.bus.Request(ctx, msg)
	if err != nil {
		RespondError(w, 503, "CURATOR_UNAVAILABLE", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}
