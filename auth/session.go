package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SessionManager handles JWT token creation/validation and API keys.
type SessionManager struct {
	secret  []byte
	mu      sync.RWMutex
	apiKeys map[string]Principal // key hash -> principal
}

// NewSessionManager creates a session manager with the given signing secret.
func NewSessionManager(secret string) *SessionManager {
	return &SessionManager{
		secret:  []byte(secret),
		apiKeys: make(map[string]Principal),
	}
}

// jwtHeader is the fixed JWT header for HS256.
var jwtHeader = base64url([]byte(`{"alg":"HS256","typ":"JWT"}`))

type jwtClaims struct {
	Sub    string   `json:"sub"`
	Tenant string   `json:"tenant"`
	Type   int      `json:"type"`
	Roles  []string `json:"roles"`
	Exp    int64    `json:"exp"`
	Iat    int64    `json:"iat"`
}

// CreateToken creates a signed JWT for the given principal.
func (sm *SessionManager) CreateToken(p Principal, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwtClaims{
		Sub:    p.ID,
		Tenant: p.Tenant,
		Type:   int(p.Type),
		Roles:  p.Roles,
		Iat:    now.Unix(),
		Exp:    now.Add(ttl).Unix(),
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	payload := base64url(claimsJSON)
	signingInput := jwtHeader + "." + payload

	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(signingInput))
	signature := base64url(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

// ValidateToken validates a JWT and returns the principal.
func (sm *SessionManager) ValidateToken(token string) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, fmt.Errorf("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64url(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return Principal{}, fmt.Errorf("invalid token signature")
	}

	claimsJSON, err := base64urlDecode(parts[1])
	if err != nil {
		return Principal{}, fmt.Errorf("decode claims: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return Principal{}, fmt.Errorf("parse claims: %w", err)
	}

	if time.Now().Unix() > claims.Exp {
		return Principal{}, fmt.Errorf("token expired")
	}

	return Principal{
		Type:   PrincipalType(claims.Type),
		ID:     claims.Sub,
		Tenant: claims.Tenant,
		Roles:  claims.Roles,
	}, nil
}

// CreateAPIKey generates a random API key and associates it with a principal.
func (sm *SessionManager) CreateAPIKey(name string, p Principal) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}

	key := "cyntr_" + hex.EncodeToString(buf)

	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	sm.mu.Lock()
	sm.apiKeys[keyHash] = p
	sm.mu.Unlock()

	return key, nil
}

// ValidateAPIKey checks an API key and returns the associated principal.
func (sm *SessionManager) ValidateAPIKey(key string) (Principal, error) {
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	sm.mu.RLock()
	p, ok := sm.apiKeys[keyHash]
	sm.mu.RUnlock()

	if !ok {
		return Principal{}, fmt.Errorf("invalid API key")
	}
	return p, nil
}

func base64url(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func base64urlDecode(s string) ([]byte, error) {
	// Add padding back
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
