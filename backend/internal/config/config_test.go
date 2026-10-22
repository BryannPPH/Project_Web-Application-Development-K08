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
	if cfg.Authentication.SessionIdleTimeout != 30*time.Minute {
		t.Errorf("Authentication.SessionIdleTimeout = %v, want 30m", cfg.Authentication.SessionIdleTimeout)
	}
	if cfg.Authentication.SessionAbsoluteTimeout != 12*time.Hour {
		t.Errorf("Authentication.SessionAbsoluteTimeout = %v, want 12h", cfg.Authentication.SessionAbsoluteTimeout)
	}
	if cfg.Authentication.SessionRotationInterval != 15*time.Minute {
		t.Errorf("Authentication.SessionRotationInterval = %v, want 15m", cfg.Authentication.SessionRotationInterval)
	}
	if cfg.Authentication.SessionPreviousTokenGrace != 30*time.Second {
		t.Errorf("Authentication.SessionPreviousTokenGrace = %v, want 30s", cfg.Authentication.SessionPreviousTokenGrace)
	}
	if cfg.Authentication.SessionCookieName != "dbmp_session" {
		t.Errorf("Authentication.SessionCookieName = %q, want dbmp_session", cfg.Authentication.SessionCookieName)
	}
	if cfg.Authentication.SessionCookieSecure {
		t.Error("Authentication.SessionCookieSecure = true in development, want false")
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
		{name: "idle exceeds absolute", env: map[string]string{"SESSION_IDLE_TIMEOUT": "13h"}},
		{name: "rotation exceeds absolute", env: map[string]string{"SESSION_ROTATION_INTERVAL": "13h"}},
		{name: "grace exceeds rotation", env: map[string]string{"SESSION_PREVIOUS_TOKEN_GRACE": "16m"}},
		{name: "invalid secure boolean", env: map[string]string{"SESSION_COOKIE_SECURE": "sometimes"}},
		{name: "invalid cookie name", env: map[string]string{"SESSION_COOKIE_NAME": "bad cookie"}},
		{name: "insecure production cookie", env: map[string]string{"APP_ENV": "production", "SESSION_COOKIE_SECURE": "false"}},
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

func TestLoadEnablesSecureCookieByDefaultInProduction(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapEnvironment(map[string]string{
		"APP_ENV":               "production",
		"PLATFORM_DATABASE_URL": "postgres://platform:secret@localhost:5432/platform_db",
	}))
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if !cfg.Authentication.SessionCookieSecure {
		t.Error("Authentication.SessionCookieSecure = false, want true")
	}
}

func mapEnvironment(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, exists := values[key]
		return value, exists
	}
}
