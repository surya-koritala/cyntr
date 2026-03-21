package api

import (
	"net/http"
	"sync/atomic"
	"time"
)

type MetricsCollector struct {
	requestCount atomic.Int64
	errorCount   atomic.Int64
	totalLatency atomic.Int64
	startTime    time.Time
}

var metrics = &MetricsCollector{startTime: time.Now()}

func (mc *MetricsCollector) RecordRequest(duration time.Duration, isError bool) {
	mc.requestCount.Add(1)
	mc.totalLatency.Add(duration.Milliseconds())
	if isError {
		mc.errorCount.Add(1)
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	reqs := metrics.requestCount.Load()
	errs := metrics.errorCount.Load()
	totalLat := metrics.totalLatency.Load()
	var avgLat int64
	if reqs > 0 {
		avgLat = totalLat / reqs
	}

	Respond(w, 200, map[string]any{
		"requests_total": reqs,
		"errors_total":   errs,
		"avg_latency_ms": avgLat,
		"uptime_seconds": int(time.Since(metrics.startTime).Seconds()),
	})
}
