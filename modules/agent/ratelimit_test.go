package agent

import "testing"

func TestRateLimitUnlimited(t *testing.T) {
	err := checkAgentRateLimit("test/unlimited", 0)
	if err != nil {
		t.Fatalf("unlimited should not error: %v", err)
	}
}

func TestRateLimitEnforced(t *testing.T) {
	key := "test/limited"
	// Reset
	rateLimiterMu.Lock()
	delete(rateLimiters, key)
	rateLimiterMu.Unlock()

	for i := 0; i < 5; i++ {
		err := checkAgentRateLimit(key, 5)
		if err != nil {
			t.Fatalf("request %d should succeed: %v", i, err)
		}
	}
	err := checkAgentRateLimit(key, 5)
	if err == nil {
		t.Fatal("6th request should be rate limited")
	}
}
