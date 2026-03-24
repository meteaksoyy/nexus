package ibkr

import "sync"

// SessionState represents the authentication state of the IBKR gateway session.
type SessionState int

const (
	StateUnknown         SessionState = iota // not yet checked
	StateUnauthenticated                     // gateway running but not logged in
	StateAuthenticated                       // session active
)

func (s SessionState) String() string {
	switch s {
	case StateAuthenticated:
		return "authenticated"
	case StateUnauthenticated:
		return "unauthenticated"
	default:
		return "unknown"
	}
}

// SessionManager is a thread-safe holder for the current session state.
type SessionManager struct {
	mu    sync.RWMutex
	state SessionState
}

func newSessionManager() *SessionManager {
	return &SessionManager{state: StateUnknown}
}

func (s *SessionManager) Set(state SessionState) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

func (s *SessionManager) Get() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *SessionManager) IsAuthenticated() bool {
	return s.Get() == StateAuthenticated
}
