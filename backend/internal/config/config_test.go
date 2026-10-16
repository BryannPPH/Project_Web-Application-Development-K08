package config

import (
	"testing"
	"time"
)

func TestLoadUsesSafeDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapEnvironment(map[string]string{
		"PLATFORM_DATABASE_URL": "postgres://platform:secret@localhost:5432/platform_db",
	}))
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
	if cfg.PlatformDatabase.MinConnections != 2 {
		t.Errorf("PlatformDatabase.MinConnections = %d, want 2", cfg.PlatformDatabase.MinConnections)
	}
	if cfg.PlatformDatabase.MaxConnections != 10 {
		t.Errorf("PlatformDatabase.MaxConnections = %d, want 10", cfg.PlatformDatabase.MaxConnections)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "missing platform database URL", env: map[string]string{}},
		{name: "unknown environment", env: map[string]string{"APP_ENV": "staging"}},
		{name: "empty address", env: map[string]string{"HTTP_ADDRESS": "  "}},
		{name: "invalid duration", env: map[string]string{"HTTP_READ_TIMEOUT": "soon"}},
		{name: "non-positive duration", env: map[string]string{"HTTP_IDLE_TIMEOUT": "0s"}},
		{name: "invalid log level", env: map[string]string{"LOG_LEVEL": "verbose"}},
		{name: "invalid max connections", env: map[string]string{"PLATFORM_DB_MAX_CONNECTIONS": "many"}},
		{name: "min exceeds max", env: map[string]string{"PLATFORM_DB_MIN_CONNECTIONS": "11"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			environment := map[string]string{
				"PLATFORM_DATABASE_URL": "postgres://platform:secret@localhost:5432/platform_db",
			}
			for key, value := range tt.env {
				environment[key] = value
			}
			if tt.name == "missing platform database URL" {
				delete(environment, "PLATFORM_DATABASE_URL")
			}

			_, err := load(mapEnvironment(environment))
			if err == nil {
				t.Fatal("load() error = nil, want an error")
			}
		})
	}
}

func mapEnvironment(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, exists := values[key]
		return value, exists
	}
}
