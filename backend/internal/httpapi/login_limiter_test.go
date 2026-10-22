package httpapi

import (
	"errors"
	"testing"
	"time"
)

func TestLoginLimiterAppliesUsernameAndClientLimits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	limiter, err := newFixedWindowLoginLimiter(2, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newFixedWindowLoginLimiter() error = %v", err)
	}

	if allowed, _ := limiter.Allow("Alice", "192.0.2.1"); !allowed {
		t.Fatal("first attempt was rejected")
	}
	if allowed, _ := limiter.Allow("alice", "192.0.2.2"); !allowed {
		t.Fatal("second attempt was rejected")
	}
	if allowed, retryAfter := limiter.Allow(" ALICE ", "192.0.2.3"); allowed || retryAfter != time.Minute {
		t.Fatalf("username limit result = (%v, %v), want (false, 1m)", allowed, retryAfter)
	}

	clientLimiter, err := newFixedWindowLoginLimiter(2, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newFixedWindowLoginLimiter() error = %v", err)
	}
	_, _ = clientLimiter.Allow("alice", "192.0.2.1")
	_, _ = clientLimiter.Allow("bob", "192.0.2.1")
	if allowed, _ := clientLimiter.Allow("charlie", "192.0.2.1"); allowed {
		t.Fatal("client limit did not reject the third attempt")
	}
}

func TestLoginLimiterResetsAfterWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	limiter, err := newFixedWindowLoginLimiter(1, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newFixedWindowLoginLimiter() error = %v", err)
	}
	_, _ = limiter.Allow("alice", "192.0.2.1")
	if allowed, _ := limiter.Allow("alice", "192.0.2.1"); allowed {
		t.Fatal("attempt inside active window was allowed")
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("alice", "192.0.2.1"); !allowed {
		t.Fatal("attempt after window reset was rejected")
	}
}

func TestLoginLimiterRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := NewFixedWindowLoginLimiter(0, time.Minute); !errors.Is(err, ErrInvalidLoginRateLimit) {
		t.Fatalf("configuration error = %v, want ErrInvalidLoginRateLimit", err)
	}
}
