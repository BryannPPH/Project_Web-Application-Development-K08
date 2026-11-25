package migration

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const recoveryReason = "migration execution was interrupted by a server restart; manual reconciliation is required"

// recoverRunning remains an explicit administrative recovery primitive. API
// startup must not call it because a worker may still own the database lock.
func recoverRunning(ctx context.Context, repository Repository, logger *slog.Logger, before time.Time) error {
	recovered, err := repository.RecoverRunning(ctx, before, recoveryReason)
	if err != nil {
		return fmt.Errorf("recover interrupted migrations: %w", err)
	}
	if recovered > 0 {
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(ctx, "recovered interrupted migrations", "count", recovered)
	}
	return nil
}
