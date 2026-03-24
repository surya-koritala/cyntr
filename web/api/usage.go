package api

import (
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

var usageStore *agent.UsageStore

func SetUsageStore(store *agent.UsageStore) {
	usageStore = store
}

func (s *Server) handleUsageQuery(w http.ResponseWriter, r *http.Request) {
	if usageStore == nil {
		Respond(w, 200, []any{})
		return
	}

	tenant := r.URL.Query().Get("tenant")
	agentName := r.URL.Query().Get("agent")
	var since, until time.Time
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		since, _ = time.Parse(time.RFC3339, sinceStr)
	}
	if untilStr := r.URL.Query().Get("until"); untilStr != "" {
		until, _ = time.Parse(time.RFC3339, untilStr)
	}

	records, err := usageStore.Query(tenant, agentName, since, until)
	if err != nil {
		RespondError(w, 500, "USAGE_ERROR", err.Error())
		return
	}
	Respond(w, 200, records)
}

func (s *Server) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	if usageStore == nil {
		Respond(w, 200, []any{})
		return
	}

	tenant := r.URL.Query().Get("tenant")
	summaries, err := usageStore.Summarize(tenant)
	if err != nil {
		RespondError(w, 500, "USAGE_ERROR", err.Error())
		return
	}
	Respond(w, 200, summaries)
}
