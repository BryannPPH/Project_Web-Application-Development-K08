package config

import (
	"testing"
	"time"
)

func TestLoadUsesSafeDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load(emptyEnvironment)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}

	if cfg.Environment != EnvironmentDevelopment {
		t.Errorf("Environment = %q, want %q", cfg.Environment, EnvironmentDevelopment)
	}
	if cfg.HTTP.Address != ":8080" {
		t.Errorf("HTTP.Address = %q, want %q", cfg.HTTP.Address, ":8080")
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("HTTP.ReadHeaderTimeout = %v, want %v", cfg.HTTP.ReadHeaderTimeout, 5*time.Second)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "unknown environment", env: map[string]string{"APP_ENV": "staging"}},
		{name: "empty address", env: map[string]string{"HTTP_ADDRESS": "  "}},
		{name: "invalid duration", env: map[string]string{"HTTP_READ_TIMEOUT": "soon"}},
		{name: "non-positive duration", env: map[string]string{"HTTP_IDLE_TIMEOUT": "0s"}},
		{name: "invalid log level", env: map[string]string{"LOG_LEVEL": "verbose"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := load(mapEnvironment(tt.env))
			if err == nil {
				t.Fatal("load() error = nil, want an error")
			}
		})
	}
}

func emptyEnvironment(string) (string, bool) {
	return "", false
}

func mapEnvironment(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, exists := values[key]
		return value, exists
	}
}
