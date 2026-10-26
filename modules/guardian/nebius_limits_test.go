package guardian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidationCapsModelOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Error(err) }
		if body["max_tokens"] != float64(1024) { t.Errorf("expected explicit 1024-token budget, got %v", body["max_tokens"]) }
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"action\":\"restore_replicas\",\"desired_replicas\":1,\"explanation\":\"test fixture\"}"}}]}`)
	}))
	defer server.Close()
	p := NebiusPlanner{APIKey:"test-only", BaseURL:server.URL+"/v1", Client:server.Client()}
	if _, err := p.Propose(context.Background(), Incident{}); err != nil { t.Fatal(err) }
}
