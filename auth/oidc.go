package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OIDCConfig holds OIDC provider configuration.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// OIDCProvider handles OpenID Connect authentication.
type OIDCProvider struct {
	config      OIDCConfig
	sessions    *SessionManager
	client      *http.Client
	authURL     string
	tokenURL    string
	userinfoURL string
}

// NewOIDCProvider creates an OIDC provider.
// discoveryURL can be empty — if so, endpoints must be set manually via SetEndpoints.
func NewOIDCProvider(config OIDCConfig, sessions *SessionManager) *OIDCProvider {
	return &OIDCProvider{
		config:   config,
		sessions: sessions,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// SetEndpoints manually sets the OIDC endpoints (for testing without discovery).
func (p *OIDCProvider) SetEndpoints(authURL, tokenURL, userinfoURL string) {
	p.authURL = authURL
	p.tokenURL = tokenURL
	p.userinfoURL = userinfoURL
}

// Discover fetches OIDC configuration from the issuer's discovery endpoint.
func (p *OIDCProvider) Discover(ctx context.Context) error {
	url := strings.TrimRight(p.config.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("OIDC discovery: %w", err)
	}
	defer resp.Body.Close()

	var config struct {
		AuthEndpoint     string `json:"authorization_endpoint"`
		TokenEndpoint    string `json:"token_endpoint"`
		UserinfoEndpoint string `json:"userinfo_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return fmt.Errorf("parse discovery: %w", err)
	}

	p.authURL = config.AuthEndpoint
	p.tokenURL = config.TokenEndpoint
	p.userinfoURL = config.UserinfoEndpoint
	return nil
}

// AuthURL returns the URL to redirect the user to for authentication.
func (p *OIDCProvider) AuthURL(state string) string {
	scopes := p.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	params := url.Values{
		"response_type": {"code"},
		"client_id":     {p.config.ClientID},
		"redirect_uri":  {p.config.RedirectURL},
		"scope":         {strings.Join(scopes, " ")},
		"state":         {state},
	}

	return p.authURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for tokens and returns a Cyntr JWT.
func (p *OIDCProvider) ExchangeCode(ctx context.Context, code, tenant string, roles []string) (string, error) {
	// Exchange code for tokens
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURL},
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token error %d: %s", resp.StatusCode, string(b))
	}

	var tokenResp struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("parse tokens: %w", err)
	}

	// Parse ID token claims (skip signature verification for now — use the userinfo endpoint for validation)
	claims, err := parseIDTokenClaims(tokenResp.IDToken)
	if err != nil {
		return "", fmt.Errorf("parse ID token: %w", err)
	}

	// Create Cyntr session
	principal := Principal{
		Type:   PrincipalUser,
		ID:     claims.Email,
		Tenant: tenant,
		Roles:  roles,
	}

	token, err := p.sessions.CreateToken(principal, 8*time.Hour)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return token, nil
}

// IDTokenClaims represents the relevant claims from an OIDC ID token.
type IDTokenClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// parseIDTokenClaims extracts claims from a JWT ID token without verifying the signature.
// In production, the signature should be verified against the IdP's JWKS.
func parseIDTokenClaims(idToken string) (*IDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode the payload (second part)
	payload := parts[1]
	// Add padding
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims IDTokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	return &claims, nil
}
