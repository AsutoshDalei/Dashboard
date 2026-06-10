package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	os.Setenv("ACCESS_PASSKEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != "5001" {
		t.Errorf("Port = %q, want %q", cfg.Port, "5001")
	}
	if cfg.LLMProvider != "openrouter" {
		t.Errorf("LLMProvider = %q, want %q", cfg.LLMProvider, "openrouter")
	}
}

func TestValidateMissingRequired(t *testing.T) {
	os.Clearenv()

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing required vars")
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"false", false},
		{"0", false},
		{"", false},
		{"random", false},
	}

	for _, tt := range tests {
		os.Setenv("TEST_BOOL", tt.val)
		got := getEnvBool("TEST_BOOL")
		if got != tt.want {
			t.Errorf("getEnvBool(%q) = %v, want %v", tt.val, got, tt.want)
		}
	}
}