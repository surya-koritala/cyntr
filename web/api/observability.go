package api

import (
	"math"
	"net/http"
	"sort"
	"time"
)

// LatencyPercentiles holds latency percentile data for a single agent.
type LatencyPercentiles struct {
	Agent       string `json:"agent"`
	P50Ms       int64  `json:"p50_ms"`
	P95Ms       int64  `json:"p95_ms"`
	P99Ms       int64  `json:"p99_ms"`
	SampleCount int    `json:"sample_count"`
}

// TokenHourBucket holds token usage aggregated into an hourly bucket.
type TokenHourBucket struct {
	Hour         string `json:"hour"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
}

func (s *Server) handleObservabilityLatency(w http.ResponseWriter, r *http.Request) {
	if usageStore == nil {
		Respond(w, 200, []any{})
		return
	}

	tenant := r.URL.Query().Get("tenant")
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	records, err := usageStore.Query(tenant, "", since, now)
	if err != nil {
		RespondError(w, 500, "OBSERVABILITY_ERROR", err.Error())
		return
	}

	// Group durations by agent.
	agentDurations := make(map[string][]int64)
	for _, rec := range records {
		agentDurations[rec.Agent] = append(agentDurations[rec.Agent], rec.DurationMs)
	}

	var results []LatencyPercentiles
	for agentName, durations := range agentDurations {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		n := len(durations)
		results = append(results, LatencyPercentiles{
			Agent:       agentName,
			P50Ms:       percentile(durations, n, 50),
			P95Ms:       percentile(durations, n, 95),
			P99Ms:       percentile(durations, n, 99),
			SampleCount: n,
		})
	}

	// Sort results by agent name for deterministic output.
	sort.Slice(results, func(i, j int) bool { return results[i].Agent < results[j].Agent })

	if results == nil {
		results = []LatencyPercentiles{}
	}
	Respond(w, 200, results)
}

// percentile returns the p-th percentile value from a sorted slice of durations.
func percentile(sorted []int64, n int, p int) int64 {
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := int(math.Ceil(float64(p)/100.0*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func (s *Server) handleObservabilityTokens(w http.ResponseWriter, r *http.Request) {
	if usageStore == nil {
		Respond(w, 200, []any{})
		return
	}

	tenant := r.URL.Query().Get("tenant")
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	records, err := usageStore.Query(tenant, "", since, now)
	if err != nil {
		RespondError(w, 500, "OBSERVABILITY_ERROR", err.Error())
		return
	}

	// Group by truncated hour.
	type bucket struct {
		input  int
		output int
	}
	hourMap := make(map[string]*bucket)
	var hourKeys []string

	for _, rec := range records {
		hourKey := rec.Timestamp.UTC().Truncate(time.Hour).Format(time.RFC3339)
		b, ok := hourMap[hourKey]
		if !ok {
			b = &bucket{}
			hourMap[hourKey] = b
			hourKeys = append(hourKeys, hourKey)
		}
		b.input += rec.InputTokens
		b.output += rec.OutputTokens
	}

	sort.Strings(hourKeys)

	results := make([]TokenHourBucket, 0, len(hourKeys))
	for _, h := range hourKeys {
		b := hourMap[h]
		results = append(results, TokenHourBucket{
			Hour:         h,
			InputTokens:  b.input,
			OutputTokens: b.output,
			TotalTokens:  b.input + b.output,
		})
	}

	Respond(w, 200, results)
}

func (s *Server) handleObservabilityTools(w http.ResponseWriter, r *http.Request) {
	if usageStore == nil {
		Respond(w, 200, []any{})
		return
	}

	tenant := r.URL.Query().Get("tenant")
	summaries, err := usageStore.Summarize(tenant)
	if err != nil {
		RespondError(w, 500, "OBSERVABILITY_ERROR", err.Error())
		return
	}
	Respond(w, 200, summaries)
}
