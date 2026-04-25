package agent

import (
	"sync"
	"time"
)

// Session represents a conversation session with history.
type Session struct {
	History   []Message
	UpdatedAt time.Time
}

// SessionManager manages conversation sessions keyed by thread.
// Key format: "channelID:threadTS" (threadTS can be empty for non-threaded)
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionManager creates a new session manager.
// Sessions expire after ttl of inactivity.
func NewSessionManager(ttl time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
	go sm.cleanupLoop()
	return sm
}

// sessionKey generates a key for a conversation.
// For threaded messages: channelID:threadTS
// For non-threaded: channelID:messageTS (creates new thread)
func SessionKey(channelID, threadTS string) string {
	if threadTS == "" {
		return channelID + ":"
	}
	return channelID + ":" + threadTS
}

// GetHistory returns the conversation history for a session.
func (sm *SessionManager) GetHistory(key string) []Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if s, ok := sm.sessions[key]; ok {
		// Return a copy to avoid race conditions
		history := make([]Message, len(s.History))
		copy(history, s.History)
		return history
	}
	return nil
}

// UpdateHistory replaces the session history.
func (sm *SessionManager) UpdateHistory(key string, history []Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.sessions[key] = &Session{
		History:   history,
		UpdatedAt: time.Now(),
	}
}

// ClearSession removes a session.
func (sm *SessionManager) ClearSession(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, key)
}

// cleanupLoop periodically removes expired sessions.
func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(sm.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		sm.cleanup()
	}
}

func (sm *SessionManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-sm.ttl)
	for key, s := range sm.sessions {
		if s.UpdatedAt.Before(cutoff) {
			delete(sm.sessions, key)
		}
	}
}
