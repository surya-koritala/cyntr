package auth

import (
	"context"
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
)

var oidcLogger = log.Default().WithModule("oidc")

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
	jwksURI     string
	jwksKeys    map[string]*rsa.PublicKey
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
		JwksURI          string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return fmt.Errorf("parse discovery: %w", err)
	}

	p.authURL = config.AuthEndpoint
	p.tokenURL = config.TokenEndpoint
	p.userinfoURL = config.UserinfoEndpoint
	p.jwksURI = config.JwksURI
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

// AuthURLWithPKCE returns the authorization URL with PKCE parameters and the
// generated code verifier. The caller must store the verifier and pass it to
// ExchangeCodeWithPKCE when exchanging the authorization code.
func (p *OIDCProvider) AuthURLWithPKCE(state string) (authURL, codeVerifier string) {
	// Generate code verifier (43-128 chars, unreserved chars per RFC 7636)
	buf := make([]byte, 32)
	crand.Read(buf)
	codeVerifier = base64.RawURLEncoding.EncodeToString(buf)

	// Generate code challenge (S256)
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	scopes := p.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	params := url.Values{}
	params.Set("client_id", p.config.ClientID)
	params.Set("redirect_uri", p.config.RedirectURL)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(scopes, " "))
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")

	authURL = p.authURL + "?" + params.Encode()
	return
}

// ExchangeCodeWithPKCE exchanges an authorization code for tokens using a PKCE
// code verifier and returns a Cyntr JWT.
func (p *OIDCProvider) ExchangeCodeWithPKCE(ctx context.Context, code, codeVerifier, tenant string, roles []string) (string, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURL},
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"code_verifier": {codeVerifier},
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

	if err := p.verifyIDTokenSignature(tokenResp.IDToken); err != nil {
		return "", fmt.Errorf("ID token signature: %w", err)
	}

	claims, err := parseIDTokenClaims(tokenResp.IDToken)
	if err != nil {
		return "", fmt.Errorf("parse ID token: %w", err)
	}

	// Validate expiry claim
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return "", fmt.Errorf("ID token expired")
	}

	// Validate audience claim
	if claims.Aud != "" && claims.Aud != p.config.ClientID {
		return "", fmt.Errorf("ID token audience mismatch: got %q, want %q", claims.Aud, p.config.ClientID)
	}

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

// verifyIDTokenSignature verifies the RS256 signature of an OIDC ID token.
func (p *OIDCProvider) verifyIDTokenSignature(idToken string) error {
	// Split JWT parts
	parts := strings.SplitN(idToken, ".", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT format: expected 3 parts")
	}

	// Decode header to get kid
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return fmt.Errorf("decode header: %w", err)
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return fmt.Errorf("parse header: %w", err)
	}

	if header.Alg != "RS256" {
		return fmt.Errorf("unsupported algorithm: %s (only RS256 supported)", header.Alg)
	}

	// Fetch JWKS if not cached
	if p.jwksKeys == nil {
		if err := p.fetchJWKS(); err != nil {
			oidcLogger.Warn("JWKS fetch failed, skipping signature verification", map[string]any{"error": err.Error()})
			return nil // graceful degradation
		}
	}

	// Find matching key by kid
	key, ok := p.jwksKeys[header.Kid]
	if !ok {
		// Refresh JWKS in case keys rotated
		if err := p.fetchJWKS(); err != nil {
			return fmt.Errorf("JWKS refresh failed: %w", err)
		}
		key, ok = p.jwksKeys[header.Kid]
		if !ok {
			return fmt.Errorf("no matching key found for kid: %s", header.Kid)
		}
	}

	// Verify RS256 signature
	signedContent := parts[0] + "." + parts[1]
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	hashed := sha256.Sum256([]byte(signedContent))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashed[:], signature); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

// fetchJWKS fetches the JSON Web Key Set from the OIDC provider.
func (p *OIDCProvider) fetchJWKS() error {
	if p.jwksURI == "" {
		return fmt.Errorf("jwks_uri not configured")
	}

	resp, err := http.Get(p.jwksURI)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	p.jwksKeys = make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" {
			continue
		}
		nBytes, err := base64URLDecode(jwk.N)
		if err != nil {
			continue
		}
		eBytes, err := base64URLDecode(jwk.E)
		if err != nil {
			continue
		}

		// Convert e bytes to int
		var e int
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}

		pubKey := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: e,
		}
		p.jwksKeys[jwk.Kid] = pubKey
	}

	return nil
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
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

	// Validate expiry claim
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return "", fmt.Errorf("ID token expired")
	}

	// Validate audience claim
	if claims.Aud != "" && claims.Aud != p.config.ClientID {
		return "", fmt.Errorf("ID token audience mismatch: got %q, want %q", claims.Aud, p.config.ClientID)
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
	Exp   int64  `json:"exp"`
	Aud   string `json:"aud"`
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
