package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// tokenServer is a fake OAuth token endpoint. It issues sequential access
// tokens so a refresh is observable, and records grant types seen.
func tokenServer(t *testing.T, grants *[]string) *httptest.Server {
	t.Helper()
	var n int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if grants != nil {
			*grants = append(*grants, r.Form.Get("grant_type"))
		}
		i := atomic.AddInt64(&n, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"AT` + strconv.FormatInt(i, 10) + `","refresh_token":"RT","expires_in":3600}`))
	}))
}

func newManager(t *testing.T, tokenURL string, now func() time.Time) (*OAuthManager, *MemoryTokenStore) {
	store := NewMemoryTokenStore()
	m := NewOAuthManager(OAuthConfig{Provider: "portal", TokenURL: tokenURL, ClientID: "cid"}, store, http.DefaultClient, now)
	return m, store
}

func TestOAuthExchangeAndAccess(t *testing.T) {
	srv := tokenServer(t, nil)
	defer srv.Close()
	cur := time.Unix(1000, 0)
	m, store := newManager(t, srv.URL, func() time.Time { return cur })

	if err := m.Exchange(context.Background(), "acme", "auth-code", "https://cb"); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok, ok := store.Load("acme", "portal"); !ok || tok.AccessToken == "" || tok.RefreshToken != "RT" {
		t.Fatalf("token not stored: %+v ok=%v", tok, ok)
	}
	at, err := m.AccessToken(context.Background(), "acme")
	if err != nil || at == "" {
		t.Fatalf("access token: %q %v", at, err)
	}
}

func TestOAuthAutoRefreshOnExpiry(t *testing.T) {
	var grants []string
	srv := tokenServer(t, &grants)
	defer srv.Close()
	cur := time.Unix(1000, 0)
	m, _ := newManager(t, srv.URL, func() time.Time { return cur })

	m.Exchange(context.Background(), "acme", "code", "https://cb") // AT1, expires in 3600s
	first, _ := m.AccessToken(context.Background(), "acme")

	cur = cur.Add(2 * time.Hour) // past expiry
	second, err := m.AccessToken(context.Background(), "acme")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if first == second {
		t.Fatalf("expired token should have refreshed to a new access token (both %q)", first)
	}
	// grants: authorization_code (exchange) then refresh_token (refresh).
	if len(grants) < 2 || grants[len(grants)-1] != "refresh_token" {
		t.Fatalf("expected a refresh_token grant, saw %v", grants)
	}
}

func TestOAuthNoRefreshWhileValid(t *testing.T) {
	var grants []string
	srv := tokenServer(t, &grants)
	defer srv.Close()
	cur := time.Unix(1000, 0)
	m, _ := newManager(t, srv.URL, func() time.Time { return cur })

	m.Exchange(context.Background(), "acme", "code", "https://cb")
	m.AccessToken(context.Background(), "acme")
	cur = cur.Add(time.Minute) // still valid
	m.AccessToken(context.Background(), "acme")
	for _, g := range grants {
		if g == "refresh_token" {
			t.Fatal("should not refresh while the token is still valid")
		}
	}
}

func TestOAuthPerTenantIsolationAndRevoke(t *testing.T) {
	srv := tokenServer(t, nil)
	defer srv.Close()
	cur := time.Unix(1000, 0)
	m, _ := newManager(t, srv.URL, func() time.Time { return cur })

	m.Exchange(context.Background(), "acme", "c1", "https://cb")
	if _, err := m.AccessToken(context.Background(), "globex"); err == nil {
		t.Fatal("a tenant that never connected should not have a token")
	}
	m.Revoke("acme")
	if _, err := m.AccessToken(context.Background(), "acme"); err == nil {
		t.Fatal("revoked tenant should no longer have a token")
	}
}

func TestOAuthExchangeErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	}))
	defer srv.Close()
	m, _ := newManager(t, srv.URL, nil)
	if err := m.Exchange(context.Background(), "acme", "bad", "https://cb"); err == nil {
		t.Fatal("a token-endpoint error should surface")
	}
}
