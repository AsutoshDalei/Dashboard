package auth

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"sync"
	"time"
)

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

const sessionDuration = 24 * time.Hour

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]time.Time),
	}
}

func (s *SessionStore) Create() string {
	token := generateToken()
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionDuration)
	s.mu.Unlock()
	return token
}

func (s *SessionStore) Validate(token string) bool {
	s.mu.RLock()
	expiry, exists := s.sessions[token]
	s.mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		s.Delete(token)
		return false
	}

	return true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *SessionStore) PruneExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, tok)
		}
	}
}

func (s *SessionStore) RunJanitor(ctx interface{ Done() <-chan struct{} }) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.PruneExpired()
		}
	}
}

func generateToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		slog.Error("crypto/rand failed", "err", err)
		os.Exit(1)
	}
	return hex.EncodeToString(bytes)
}