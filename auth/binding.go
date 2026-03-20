package auth

import (
	"sync"
)

// ChannelIdentity represents a user's identity on a specific channel.
type ChannelIdentity struct {
	Channel string // "slack", "telegram", "discord", etc.
	UserID  string // channel-specific user ID
}

// IdentityBinding maps channel identities to unified Cyntr principals.
type IdentityBinding struct {
	mu       sync.RWMutex
	bindings map[ChannelIdentity]string   // channel identity -> cyntr principal ID (email)
	reverse  map[string][]ChannelIdentity // principal ID -> all channel identities
}

func NewIdentityBinding() *IdentityBinding {
	return &IdentityBinding{
		bindings: make(map[ChannelIdentity]string),
		reverse:  make(map[string][]ChannelIdentity),
	}
}

// Bind associates a channel identity with a Cyntr principal.
func (ib *IdentityBinding) Bind(channel, userID, principalID string) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ci := ChannelIdentity{Channel: channel, UserID: userID}
	ib.bindings[ci] = principalID
	ib.reverse[principalID] = append(ib.reverse[principalID], ci)
}

// Resolve returns the Cyntr principal ID for a channel identity.
func (ib *IdentityBinding) Resolve(channel, userID string) (string, bool) {
	ib.mu.RLock()
	defer ib.mu.RUnlock()
	id, ok := ib.bindings[ChannelIdentity{Channel: channel, UserID: userID}]
	return id, ok
}

// Unbind removes a channel identity binding.
func (ib *IdentityBinding) Unbind(channel, userID string) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ci := ChannelIdentity{Channel: channel, UserID: userID}
	principalID, ok := ib.bindings[ci]
	if !ok {
		return
	}
	delete(ib.bindings, ci)

	// Remove from reverse map
	identities := ib.reverse[principalID]
	for i, id := range identities {
		if id == ci {
			ib.reverse[principalID] = append(identities[:i], identities[i+1:]...)
			break
		}
	}
}

// GetBindings returns all channel identities for a principal.
func (ib *IdentityBinding) GetBindings(principalID string) []ChannelIdentity {
	ib.mu.RLock()
	defer ib.mu.RUnlock()

	result := make([]ChannelIdentity, len(ib.reverse[principalID]))
	copy(result, ib.reverse[principalID])
	return result
}

// Count returns total number of bindings.
func (ib *IdentityBinding) Count() int {
	ib.mu.RLock()
	defer ib.mu.RUnlock()
	return len(ib.bindings)
}
