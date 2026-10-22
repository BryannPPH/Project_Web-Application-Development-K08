package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

type Config struct {
	Environment      Environment
	LogLevel         slog.Level
	HTTP             HTTPConfig
	PlatformDatabase PlatformDatabaseConfig
	Authentication   AuthenticationConfig
}

type HTTPConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

type PlatformDatabaseConfig struct {
	URL               string
	MinConnections    int32
	MaxConnections    int32
	MaxConnectionIdle time.Duration
	MaxConnectionLife time.Duration
	HealthTimeout     time.Duration
}

type AuthenticationConfig struct {
	SessionIdleTimeout        time.Duration
	SessionAbsoluteTimeout    time.Duration
	SessionRotationInterval   time.Duration
	SessionPreviousTokenGrace time.Duration
	SessionCookieName         string
	SessionCookieSecure       bool
}

type lookupEnv func(string) (string, bool)

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup lookupEnv) (Config, error) {
	environment, err := parseEnvironment(valueOrDefault(lookup, "APP_ENV", string(EnvironmentDevelopment)))
	if err != nil {
		return Config{}, err
	}

	logLevel, err := parseLogLevel(valueOrDefault(lookup, "LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}

	address := strings.TrimSpace(valueOrDefault(lookup, "HTTP_ADDRESS", ":8080"))
	if address == "" {
		return Config{}, fmt.Errorf("HTTP_ADDRESS must not be empty")
	}

	readHeaderTimeout, err := duration(lookup, "HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	readTimeout, err := duration(lookup, "HTTP_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := duration(lookup, "HTTP_WRITE_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := duration(lookup, "HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := duration(lookup, "HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	platformDatabaseURL, err := requiredValue(lookup, "PLATFORM_DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	minConnections, err := positiveInt32(lookup, "PLATFORM_DB_MIN_CONNECTIONS", 2, true)
	if err != nil {
		return Config{}, err
	}
	maxConnections, err := positiveInt32(lookup, "PLATFORM_DB_MAX_CONNECTIONS", 10, false)
	if err != nil {
		return Config{}, err
	}
	if minConnections > maxConnections {
		return Config{}, fmt.Errorf("PLATFORM_DB_MIN_CONNECTIONS must not exceed PLATFORM_DB_MAX_CONNECTIONS")
	}
	maxConnectionIdle, err := duration(lookup, "PLATFORM_DB_MAX_CONNECTION_IDLE", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	maxConnectionLife, err := duration(lookup, "PLATFORM_DB_MAX_CONNECTION_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	healthTimeout, err := duration(lookup, "PLATFORM_DB_HEALTH_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	sessionIdleTimeout, err := duration(lookup, "SESSION_IDLE_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	sessionAbsoluteTimeout, err := duration(lookup, "SESSION_ABSOLUTE_TIMEOUT", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}
	sessionRotationInterval, err := duration(lookup, "SESSION_ROTATION_INTERVAL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	sessionPreviousTokenGrace, err := duration(lookup, "SESSION_PREVIOUS_TOKEN_GRACE", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	if sessionIdleTimeout >= sessionAbsoluteTimeout {
		return Config{}, fmt.Errorf("SESSION_IDLE_TIMEOUT must be shorter than SESSION_ABSOLUTE_TIMEOUT")
	}
	if sessionRotationInterval >= sessionAbsoluteTimeout {
		return Config{}, fmt.Errorf("SESSION_ROTATION_INTERVAL must be shorter than SESSION_ABSOLUTE_TIMEOUT")
	}
	if sessionPreviousTokenGrace >= sessionRotationInterval {
		return Config{}, fmt.Errorf("SESSION_PREVIOUS_TOKEN_GRACE must be shorter than SESSION_ROTATION_INTERVAL")
	}

	sessionCookieName := strings.TrimSpace(valueOrDefault(lookup, "SESSION_COOKIE_NAME", "dbmp_session"))
	if !validCookieName(sessionCookieName) {
		return Config{}, fmt.Errorf("SESSION_COOKIE_NAME is invalid")
	}
	sessionCookieSecure, err := boolean(
		lookup,
		"SESSION_COOKIE_SECURE",
		environment == EnvironmentProduction,
	)
	if err != nil {
		return Config{}, err
	}
	if environment == EnvironmentProduction && !sessionCookieSecure {
		return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE must be true in production")
	}

	return Config{
		Environment: environment,
		LogLevel:    logLevel,
		HTTP: HTTPConfig{
			Address:           address,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
			ShutdownTimeout:   shutdownTimeout,
		},
		PlatformDatabase: PlatformDatabaseConfig{
			URL:               platformDatabaseURL,
			MinConnections:    minConnections,
			MaxConnections:    maxConnections,
			MaxConnectionIdle: maxConnectionIdle,
			MaxConnectionLife: maxConnectionLife,
			HealthTimeout:     healthTimeout,
		},
		Authentication: AuthenticationConfig{
			SessionIdleTimeout:        sessionIdleTimeout,
			SessionAbsoluteTimeout:    sessionAbsoluteTimeout,
			SessionRotationInterval:   sessionRotationInterval,
			SessionPreviousTokenGrace: sessionPreviousTokenGrace,
			SessionCookieName:         sessionCookieName,
			SessionCookieSecure:       sessionCookieSecure,
		},
	}, nil
}

func parseEnvironment(raw string) (Environment, error) {
	environment := Environment(strings.ToLower(strings.TrimSpace(raw)))
	switch environment {
	case EnvironmentDevelopment, EnvironmentTest, EnvironmentProduction:
		return environment, nil
	default:
		return "", fmt.Errorf("APP_ENV must be development, test, or production")
	}
}

func parseLogLevel(raw string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(raw)))); err != nil {
		return 0, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	return level, nil
}

func duration(lookup lookupEnv, key string, defaultValue time.Duration) (time.Duration, error) {
	raw, exists := lookup(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return value, nil
}

func valueOrDefault(lookup lookupEnv, key, defaultValue string) string {
	value, exists := lookup(key)
	if !exists {
		return defaultValue
	}
	return value
}

func requiredValue(lookup lookupEnv, key string) (string, error) {
	value, exists := lookup(key)
	if !exists || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return strings.TrimSpace(value), nil
}

func positiveInt32(lookup lookupEnv, key string, defaultValue int32, allowZero bool) (int32, error) {
	raw, exists := lookup(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer", key)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be zero or greater", key)
	}
	if !allowZero && value == 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return int32(value), nil
}

func boolean(lookup lookupEnv, key string, defaultValue bool) (bool, error) {
	raw, exists := lookup(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s must be a valid boolean", key)
	}
	return value, nil
}

func validCookieName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
