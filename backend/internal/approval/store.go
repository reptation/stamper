package approval

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid approval request")
	ErrInvalidToken   = errors.New("invalid approval token")
	ErrExpiredToken   = errors.New("approval token expired")
	ErrMethodMismatch = errors.New("approval token method mismatch")
	ErrHostMismatch   = errors.New("approval token host mismatch")
)

type Token struct {
	Value     string
	Method    string
	Host      string
	ExpiresAt time.Time
}

type Store struct {
	mu     sync.Mutex
	tokens map[string]Token
	ttl    time.Duration
	now    func() time.Time
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}

	return &Store{
		tokens: make(map[string]Token),
		ttl:    ttl,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func NormalizeMethodAndHost(method, rawURL string) (string, string, error) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == "" {
		return "", "", fmt.Errorf("%w: method is required", ErrInvalidRequest)
	}

	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", fmt.Errorf("%w: parse url: %v", ErrInvalidRequest, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("%w: url must use http or https", ErrInvalidRequest)
	}
	if parsed.Host == "" {
		return "", "", fmt.Errorf("%w: url host is required", ErrInvalidRequest)
	}

	return normalizedMethod, strings.ToLower(parsed.Host), nil
}

func (s *Store) Issue(method, rawURL string) (Token, error) {
	normalizedMethod, normalizedHost, err := NormalizeMethodAndHost(method, rawURL)
	if err != nil {
		return Token{}, err
	}

	value, err := generateToken()
	if err != nil {
		return Token{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked()

	token := Token{
		Value:     value,
		Method:    normalizedMethod,
		Host:      normalizedHost,
		ExpiresAt: s.now().Add(s.ttl),
	}
	s.tokens[value] = token

	return token, nil
}

func (s *Store) Validate(value, method, rawURL string) (Token, error) {
	normalizedMethod, normalizedHost, err := NormalizeMethodAndHost(method, rawURL)
	if err != nil {
		return Token{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[value]
	if !ok {
		return Token{}, ErrInvalidToken
	}
	if s.now().After(token.ExpiresAt) {
		delete(s.tokens, value)
		return Token{}, ErrExpiredToken
	}
	if token.Method != normalizedMethod {
		return Token{}, ErrMethodMismatch
	}
	if token.Host != normalizedHost {
		return Token{}, ErrHostMismatch
	}

	s.cleanupExpiredLocked()

	return token, nil
}

func (s *Store) cleanupExpiredLocked() {
	now := s.now()
	for value, token := range s.tokens {
		if now.After(token.ExpiresAt) {
			delete(s.tokens, value)
		}
	}
}

func generateToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate approval token: %w", err)
	}

	return hex.EncodeToString(buf), nil
}
