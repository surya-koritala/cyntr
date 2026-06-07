// Package providers — OAuth subscription login (D20).
//
// Lets a deployment authenticate to a model provider with an OAuth
// subscription (ChatGPT/Codex, a Portal-style aggregator, ...) instead of a
// pasted API key. The manager exchanges an authorization code for tokens,
// stores them per (tenant, provider), and hands out a valid access token —
// refreshing automatically when it has expired. Storage is delegated to a
// TokenStore so production can back it with the platform's encrypted secrets
// store while tests use an in-memory one.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuthToken is a stored token set for one (tenant, provider).
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Expired reports whether the access token is at or past expiry (with a small
// safety skew so we refresh slightly early).
func (t OAuthToken) Expired(now time.Time) bool {
	if t.ExpiresAt.IsZero() {
		return false // no expiry known — treat as long-lived
	}
	return !now.Add(30 * time.Second).Before(t.ExpiresAt)
}

// TokenStore persists tokens per (tenant, provider).
type TokenStore interface {
	Load(tenant, provider string) (OAuthToken, bool)
	Save(tenant, provider string, tok OAuthToken)
	Delete(tenant, provider string)
}

// MemoryTokenStore is an in-memory TokenStore (tests, single-process).
type MemoryTokenStore struct {
	mu sync.Mutex
	m  map[string]OAuthToken
}

func NewMemoryTokenStore() *MemoryTokenStore { return &MemoryTokenStore{m: map[string]OAuthToken{}} }

func tokKey(tenant, provider string) string { return tenant + "\x00" + provider }

func (s *MemoryTokenStore) Load(tenant, provider string) (OAuthToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[tokKey(tenant, provider)]
	return t, ok
}
func (s *MemoryTokenStore) Save(tenant, provider string, tok OAuthToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[tokKey(tenant, provider)] = tok
}
func (s *MemoryTokenStore) Delete(tenant, provider string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tokKey(tenant, provider))
}

// OAuthConfig is the provider's OAuth endpoint configuration.
type OAuthConfig struct {
	Provider     string // provider name the tokens belong to
	TokenURL     string
	ClientID     string
	ClientSecret string
}

// OAuthManager exchanges and refreshes tokens for one provider's OAuth config.
type OAuthManager struct {
	cfg   OAuthConfig
	store TokenStore
	http  *http.Client
	now   func() time.Time
}

// NewOAuthManager builds a manager. httpc/nowFn are injectable for tests.
func NewOAuthManager(cfg OAuthConfig, store TokenStore, httpc *http.Client, nowFn func() time.Time) *OAuthManager {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &OAuthManager{cfg: cfg, store: store, http: httpc, now: nowFn}
}

// Exchange swaps an authorization code for a token set and stores it for the
// tenant. redirectURI must match the one used to obtain the code.
func (m *OAuthManager) Exchange(ctx context.Context, tenant, code, redirectURI string) error {
	tok, err := m.tokenRequest(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		return err
	}
	m.store.Save(tenant, m.cfg.Provider, tok)
	return nil
}

// AccessToken returns a valid access token for the tenant, refreshing if the
// stored one has expired. Returns an error if the tenant has not connected.
func (m *OAuthManager) AccessToken(ctx context.Context, tenant string) (string, error) {
	tok, ok := m.store.Load(tenant, m.cfg.Provider)
	if !ok {
		return "", fmt.Errorf("oauth: %s not connected for tenant %q", m.cfg.Provider, tenant)
	}
	if !tok.Expired(m.now()) {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("oauth: %s token for tenant %q expired and has no refresh token", m.cfg.Provider, tenant)
	}
	refreshed, err := m.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
	})
	if err != nil {
		return "", err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tok.RefreshToken // many providers don't re-issue it
	}
	m.store.Save(tenant, m.cfg.Provider, refreshed)
	return refreshed.AccessToken, nil
}

// Revoke clears a tenant's stored token for this provider.
func (m *OAuthManager) Revoke(tenant string) { m.store.Delete(tenant, m.cfg.Provider) }

// tokenRequest performs an OAuth token endpoint POST and parses the response.
func (m *OAuthManager) tokenRequest(ctx context.Context, form url.Values) (OAuthToken, error) {
	form.Set("client_id", m.cfg.ClientID)
	if m.cfg.ClientSecret != "" {
		form.Set("client_secret", m.cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return OAuthToken{}, fmt.Errorf("oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	// Bound the response body to defend against an unbounded/hostile token
	// endpoint, and surface decode errors instead of ignoring them.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return OAuthToken{}, fmt.Errorf("oauth: decode token response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 300 || body.Error != "" {
		return OAuthToken{}, fmt.Errorf("oauth: token endpoint error: %s %s (status %d)", body.Error, body.ErrorDesc, resp.StatusCode)
	}
	if body.AccessToken == "" {
		return OAuthToken{}, fmt.Errorf("oauth: token endpoint returned no access_token")
	}
	tok := OAuthToken{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresIn > 0 {
		tok.ExpiresAt = m.now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return tok, nil
}
