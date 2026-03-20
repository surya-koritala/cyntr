package agent

import "sync"

// Session manages conversation history and context for a single agent interaction.
type Session struct {
	mu       sync.RWMutex
	id       string
	config   AgentConfig
	history  []Message
	store    *SessionStore
	memories string
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
	if s.store != nil {
		s.store.AppendMessage(s.id, msg)
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
		ctx = append(ctx, Message{
			Role:    RoleSystem,
			Content: systemContent,
		})
	}

	ctx = append(ctx, s.history...)
	return ctx
}
