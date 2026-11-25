package migration

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func LoadWorkerConfig() (WorkerConfig, error) {
	config := DefaultWorkerConfig()
	intValues := []struct {
		name string
		dest *int
	}{
		{"MIGRATION_WORKER_CONCURRENCY", &config.Concurrency},
		{"MIGRATION_WORKER_MAX_RETRIES", &config.MaxRetries},
	}
	for _, value := range intValues {
		raw, ok := os.LookupEnv(value.name)
		if !ok || raw == "" {
			continue
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return WorkerConfig{}, fmt.Errorf("%s must be an integer", value.name)
		}
		*value.dest = parsed
	}
	durationValues := []struct {
		name string
		dest *time.Duration
	}{
		{"MIGRATION_WORKER_POLL_INTERVAL", &config.PollInterval},
		{"MIGRATION_WORKER_LEASE_DURATION", &config.LeaseDuration},
		{"MIGRATION_WORKER_HEARTBEAT_INTERVAL", &config.HeartbeatInterval},
		{"MIGRATION_WORKER_RECOVERY_LOCK_TIMEOUT", &config.RecoveryLockTimeout},
		{"MIGRATION_WORKER_RETRY_BASE_DELAY", &config.RetryBaseDelay},
		{"MIGRATION_WORKER_RETRY_MAX_DELAY", &config.RetryMaxDelay},
	}
	for _, value := range durationValues {
		raw, ok := os.LookupEnv(value.name)
		if !ok || raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return WorkerConfig{}, fmt.Errorf("%s must be a duration", value.name)
		}
		*value.dest = parsed
	}
	if err := config.Validate(); err != nil {
		return WorkerConfig{}, err
	}
	return config, nil
}
