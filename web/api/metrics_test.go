package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestMetricsEndpoint(t *testing.T) {
	bus := ipc.NewBus()
	srv := NewServer(bus, nil)

	// Snapshot counts before our recording
	beforeReqs := metrics.requestCount.Load()
	beforeErrs := metrics.errorCount.Load()

	// Record some metrics
	metrics.RecordRequest(100*time.Millisecond, false)
	metrics.RecordRequest(200*time.Millisecond, false)
	metrics.RecordRequest(50*time.Millisecond, true)

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data["requests_total"] == nil {
		t.Fatal("missing requests_total")
	}

	// Verify our 3 requests were recorded (at least)
	total, ok := resp.Data["requests_total"].(float64)
	if !ok {
		t.Fatalf("requests_total not a number: %T", resp.Data["requests_total"])
	}
	if int64(total) < beforeReqs+3 {
		t.Fatalf("expected at least %d requests, got %v", beforeReqs+3, total)
	}

	errs, ok := resp.Data["errors_total"].(float64)
	if !ok {
		t.Fatalf("errors_total not a number: %T", resp.Data["errors_total"])
	}
	if int64(errs) < beforeErrs+1 {
		t.Fatalf("expected at least %d errors, got %v", beforeErrs+1, errs)
	}
}
