package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

var sessionStore *agent.SessionStore

func SetSessionStore(store *agent.SessionStore) {
	sessionStore = store
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	var body struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if body.Name == "" {
		RespondError(w, 400, "MISSING_NAME", "name is required")
		return
	}
	if body.Role == "" {
		body.Role = "user"
	}

	// Generate API key
	keyBuf := make([]byte, 32)
	rand.Read(keyBuf)
	apiKey := "cyntr_" + hex.EncodeToString(keyBuf)
	hash := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(hash[:])

	userID := fmt.Sprintf("usr_%d", time.Now().UnixNano())
	user := agent.User{
		ID: userID, Tenant: tid, Name: body.Name,
		Email: body.Email, Role: body.Role,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if sessionStore == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "session store not configured")
		return
	}
	if err := sessionStore.CreateUser(user, keyHash); err != nil {
		RespondError(w, 500, "CREATE_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]any{
		"user":    user,
		"api_key": apiKey,
		"message": "Save this API key — it cannot be retrieved again",
	})
}

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	if sessionStore == nil {
		Respond(w, 200, []any{})
		return
	}
	users, err := sessionStore.ListUsers(tid)
	if err != nil {
		RespondError(w, 500, "LIST_FAILED", err.Error())
		return
	}
	Respond(w, 200, users)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if sessionStore == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "session store not configured")
		return
	}
	if err := sessionStore.DeleteUser(uid); err != nil {
		RespondError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "deleted", "id": uid})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	// Extract API key from Authorization header
	auth := r.Header.Get("Authorization")
	token := auth
	if len(auth) > 7 && auth[:7] == "Bearer " {
		token = auth[7:]
	}
	if token == "" {
		RespondError(w, 401, "UNAUTHORIZED", "no API key provided")
		return
	}
	if sessionStore == nil {
		Respond(w, 200, map[string]string{"user": "admin", "role": "admin"})
		return
	}
	hash := sha256.Sum256([]byte(token))
	keyHash := hex.EncodeToString(hash[:])
	user, err := sessionStore.GetUserByKeyHash(keyHash)
	if err != nil {
		// Fallback: might be the root admin key
		Respond(w, 200, map[string]string{"user": "admin", "role": "admin"})
		return
	}
	Respond(w, 200, user)
}
