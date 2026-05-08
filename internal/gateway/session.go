package gateway

import (
	"sync"
	"time"
)

type Session struct {
	ID            string
	RemoteAddr    string
	CreatedAt     time.Time
	LastActive    time.Time
	PlayerID      string
	Authed        bool
	PendingTicket string
	limiter       *rateLimiter
	mu            sync.Mutex
}

type SessionManager struct {
	mu       sync.Mutex
	nextID   int64
	byID     map[string]*Session
	byPlayer map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		byID:     make(map[string]*Session),
		byPlayer: make(map[string]*Session),
	}
}

func (m *SessionManager) Add(remoteAddr string, rateLimit int) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	now := time.Now()
	session := &Session{
		ID:         "session_" + formatInt(m.nextID),
		RemoteAddr: remoteAddr,
		CreatedAt:  now,
		LastActive: now,
		limiter:    newRateLimiter(rateLimit, time.Second),
	}
	m.byID[session.ID] = session
	return session
}

func (m *SessionManager) BindPlayer(session *Session, playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session.mu.Lock()
	session.PlayerID = playerID
	session.Authed = true
	session.LastActive = time.Now()
	session.mu.Unlock()

	m.byPlayer[playerID] = session
}

func (m *SessionManager) Remove(session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.byID, session.ID)
	session.mu.Lock()
	playerID := session.PlayerID
	session.mu.Unlock()
	if playerID != "" && m.byPlayer[playerID] == session {
		delete(m.byPlayer, playerID)
	}
}

func (m *SessionManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}

func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActive = time.Now()
}

func (s *Session) AllowMessage() bool {
	return s.limiter.Allow(time.Now())
}

func (s *Session) Player() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.PlayerID, s.Authed
}

func (s *Session) SetPending(ticketID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PendingTicket = ticketID
}

func (s *Session) GetPending() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.PendingTicket
}

type rateLimiter struct {
	limit       int
	window      time.Duration
	windowStart time.Time
	count       int
	mu          sync.Mutex
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	if limit <= 0 {
		limit = 1
	}
	return &rateLimiter{limit: limit, window: window}
}

func (r *rateLimiter) Allow(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.windowStart.IsZero() || now.Sub(r.windowStart) >= r.window {
		r.windowStart = now
		r.count = 0
	}
	if r.count >= r.limit {
		return false
	}
	r.count++
	return true
}

func formatInt(value int64) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
