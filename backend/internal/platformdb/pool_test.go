package platformdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
)

func TestOpenRejectsInvalidConfigurationWithoutLeakingIt(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), config.PlatformDatabaseConfig{
		URL:               "://platform:super-secret@localhost/database",
		MinConnections:    1,
		MaxConnections:    2,
		MaxConnectionIdle: time.Minute,
		MaxConnectionLife: time.Minute,
		HealthTimeout:     time.Second,
	})

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Open() error = %v, want ErrInvalidConfiguration", err)
	}
	if err.Error() != ErrInvalidConfiguration.Error() {
		t.Errorf("Open() exposed configuration details: %q", err)
	}
}
