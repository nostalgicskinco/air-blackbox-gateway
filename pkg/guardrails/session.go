package guardrails

import (
	"sync"
	"time"
)

// SessionState tracks metrics for one agent session.
type SessionState struct {
	SessionID  string
	CreatedAt  time.Time
	LastActive time.Time

	// Token tracking
	TotalTokens  int
	RequestCount int

	// Prompt history (last N prompts for loop detection)
	PromptHistory []promptEntry

	// Tool call tracking: tool name → list of call timestamps
	ToolCalls map[string][]time.Time

	// Error tracking
	ConsecutiveErrors int
}

type promptEntry struct {
	Text      string
	Timestamp time.Time
}

// Manager holds all active sessions with automatic cleanup.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*SessionState
	ttl      time.Duration
}

// NewManager creates a session manager that cleans up sessions
// idle for longer than ttl.
func NewManager(ttl time.Duration) *Manager {
	m := &Manager{
		sessions: make(map[string]*SessionState),
		ttl:      ttl,
	}
	go m.cleanupLoop()
	return m
}

// GetOrCreate returns the session for the given ID, creating one if needed.
func (m *Manager) GetOrCreate(sessionID string) *SessionState {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		s = &SessionState{
			SessionID: sessionID,
			CreatedAt: time.Now(),
			ToolCalls: make(map[string][]time.Time),
		}
		m.sessions[sessionID] = s
	}
	return s
}

// RecordRequest updates the session after a request is parsed (before forwarding).
// This is called pre-upstream to track prompts and detect loops.
func (m *Manager) RecordRequest(sessionID string, promptText string, toolNames []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		return
	}

	now := time.Now()
	s.LastActive = now
	s.RequestCount++

	// Track prompt
	if promptText != "" {
		s.PromptHistory = append(s.PromptHistory, promptEntry{
			Text:      promptText,
			Timestamp: now,
		})
		// Keep only last 20 prompts
		if len(s.PromptHistory) > 20 {
			s.PromptHistory = s.PromptHistory[len(s.PromptHistory)-20:]
		}
	}

	// Track tool calls
	for _, tool := range toolNames {
		s.ToolCalls[tool] = append(s.ToolCalls[tool], now)
	}
}

// RecordResponse updates the session after receiving the upstream response.
func (m *Manager) RecordResponse(sessionID string, tokens int, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		return
	}

	s.TotalTokens += tokens

	if isError {
		s.ConsecutiveErrors++
	} else {
		s.ConsecutiveErrors = 0
	}
}

// GetSessionTokens returns the total tokens for a session, or 0 if not found.
func (m *Manager) GetSessionTokens(sessionID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok {
		return s.TotalTokens
	}
	return 0
}

// Remove deletes a session (used when a guardrail terminates it).
func (m *Manager) Remove(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// cleanupLoop removes idle sessions every minute.
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for id, s := range m.sessions {
			if now.Sub(s.LastActive) > m.ttl {
				delete(m.sessions, id)
			}
		}
		m.mu.Unlock()
	}
}
