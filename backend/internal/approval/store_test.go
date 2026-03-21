package approval

import (
	"errors"
	"testing"
	"time"
)

func TestIssueAndValidate(t *testing.T) {
	store := NewStore(60 * time.Second)
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	token, err := store.Issue("get", "https://example.com/data")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if token.Value == "" {
		t.Fatal("expected token value to be set")
	}
	if token.Method != "GET" {
		t.Fatalf("expected method GET, got %q", token.Method)
	}
	if token.Host != "example.com" {
		t.Fatalf("expected host example.com, got %q", token.Host)
	}

	validated, err := store.Validate(token.Value, "GET", "https://example.com/other")
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if validated.Value != token.Value {
		t.Fatalf("expected token %q, got %q", token.Value, validated.Value)
	}
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	store := NewStore(10 * time.Second)
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	token, err := store.Issue("GET", "https://example.com/data")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	store.now = func() time.Time { return now.Add(11 * time.Second) }

	_, err = store.Validate(token.Value, "GET", "https://example.com/data")
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected expired token, got %v", err)
	}
}

func TestValidateRejectsMethodAndHostMismatch(t *testing.T) {
	store := NewStore(60 * time.Second)
	token, err := store.Issue("GET", "https://example.com/data")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	_, err = store.Validate(token.Value, "POST", "https://example.com/data")
	if !errors.Is(err, ErrMethodMismatch) {
		t.Fatalf("expected method mismatch, got %v", err)
	}

	_, err = store.Validate(token.Value, "GET", "https://api.example.com/data")
	if !errors.Is(err, ErrHostMismatch) {
		t.Fatalf("expected host mismatch, got %v", err)
	}
}
