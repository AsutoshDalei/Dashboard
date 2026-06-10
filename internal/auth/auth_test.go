package auth

import (
	"testing"
)

func TestConstantTimePasskeyEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"", "", true},
		{"a", "", false},
		{"", "a", false},
		{"longerkey123", "longerkey123", true},
		{"longerkey123", "longerkey124", false},
	}

	for _, tt := range tests {
		got := ConstantTimePasskeyEqual(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("ConstantTimePasskeyEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSessionStoreCreateAndValidate(t *testing.T) {
	store := NewSessionStore()

	token := store.Create()
	if token == "" {
		t.Fatal("empty token")
	}

	if !store.Validate(token) {
		t.Error("Validate returned false for valid token")
	}
}

func TestSessionStoreDelete(t *testing.T) {
	store := NewSessionStore()

	token := store.Create()
	store.Delete(token)

	if store.Validate(token) {
		t.Error("Validate returned true for deleted token")
	}
}

func TestSessionStoreInvalidToken(t *testing.T) {
	store := NewSessionStore()

	if store.Validate("nonexistent") {
		t.Error("Validate returned true for nonexistent token")
	}
}

func TestGenerateToken(t *testing.T) {
	token := generateToken()
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	token2 := generateToken()
	if token == token2 {
		t.Error("tokens should be unique")
	}
}