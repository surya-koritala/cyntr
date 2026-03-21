package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session manages conversation history and context for a single agent interaction.
type Session struct {
	mu       sync.RWMutex
	id       string
	config   AgentConfig
	history  []Message
	store    *SessionStore
	memories string
	lastUser string
}

// SetStore attaches a SessionStore to the session for persistence.
func (s *Session) SetStore(store *SessionStore) {
	s.store = store
}

// SetMemories injects long-term memory text to be included in the agent context.
func (s *Session) SetMemories(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = text
}

// NewSession creates a new conversation session.
func NewSession(id string, config AgentConfig) *Session {
	if config.MaxTurns == 0 {
		config.MaxTurns = 10
	}
	return &Session{
		id:      id,
		config:  config,
		history: make([]Message, 0),
	}
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.id }

// Config returns the agent configuration for this session.
func (s *Session) Config() AgentConfig {
	return s.config
}

// AddMessage appends a message to the conversation history.
func (s *Session) AddMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msg)
	if s.config.MaxHistory > 0 && len(s.history) > s.config.MaxHistory {
		s.history = s.history[len(s.history)-s.config.MaxHistory:]
	}
	if s.store != nil {
		s.store.AppendMessage(s.id, msg)
	}
}

// ClearHistory resets the conversation history and removes persisted messages.
func (s *Session) ClearHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	if s.store != nil {
		s.store.ClearMessages(s.id)
	}
}

// History returns a copy of the conversation history.
func (s *Session) History() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := make([]Message, len(s.history))
	copy(h, s.history)
	return h
}

// AssembleContext builds the full message list for a model call:
// system prompt (if set) + long-term memories (if any) + conversation history.
func (s *Session) AssembleContext() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ctx []Message

	systemContent := s.config.SystemPrompt
	if s.memories != "" {
		if systemContent != "" {
			systemContent += "\n\n" + s.memories
		} else {
			systemContent = s.memories
		}
	}

	if systemContent != "" {
		systemContent = expandTemplateVars(systemContent, s.lastUser, s.config.Tenant, s.config.Name)
		ctx = append(ctx, Message{
			Role:    RoleSystem,
			Content: systemContent,
		})
	}

	ctx = append(ctx, s.history...)
	return ctx
}

// SetLastUser records the most recent user identity for template expansion.
func (s *Session) SetLastUser(user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUser = user
}

// CompactHistory replaces older messages with a condensed summary placeholder,
// keeping the last keepRecent messages.
func (s *Session) CompactHistory(keepRecent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.history) <= keepRecent {
		return
	}
	// Keep first message (usually system) and last keepRecent messages
	var compacted []Message
	compacted = append(compacted, Message{
		Role:    RoleSystem,
		Content: fmt.Sprintf("[Earlier conversation: %d messages compacted]", len(s.history)-keepRecent),
	})
	compacted = append(compacted, s.history[len(s.history)-keepRecent:]...)
	s.history = compacted
}

// expandTemplateVars replaces template placeholders in text with runtime values.
func expandTemplateVars(text, user, tenant, agentName string) string {
	now := time.Now()
	text = strings.ReplaceAll(text, "{{user}}", user)
	text = strings.ReplaceAll(text, "{{date}}", now.Format("2006-01-02"))
	text = strings.ReplaceAll(text, "{{time}}", now.Format("15:04:05"))
	text = strings.ReplaceAll(text, "{{datetime}}", now.Format("2006-01-02 15:04:05"))
	text = strings.ReplaceAll(text, "{{tenant}}", tenant)
	text = strings.ReplaceAll(text, "{{agent}}", agentName)
	return text
}
