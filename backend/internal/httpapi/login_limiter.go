package httpapi

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrInvalidLoginRateLimit = errors.New("invalid login rate limit configuration")

type LoginAttemptLimiter interface {
	Allow(username, clientID string) (bool, time.Duration)
}

type loginLimitEntry struct {
	attempts int
	resetAt  time.Time
}

// FixedWindowLoginLimiter membatasi percobaan per pengguna dan alamat klien.
type FixedWindowLoginLimiter struct {
	mutex       sync.Mutex
	entries     map[string]loginLimitEntry
	maxAttempts int
	window      time.Duration
	now         func() time.Time
	lastCleanup time.Time
}

func NewFixedWindowLoginLimiter(maxAttempts int, window time.Duration) (*FixedWindowLoginLimiter, error) {
	return newFixedWindowLoginLimiter(maxAttempts, window, time.Now)
}

func newDefaultLoginLimiter() *FixedWindowLoginLimiter {
	now := time.Now().UTC()
	return &FixedWindowLoginLimiter{
		entries:     make(map[string]loginLimitEntry),
		maxAttempts: 10,
		window:      time.Minute,
		now:         time.Now,
		lastCleanup: now,
	}
}

func newFixedWindowLoginLimiter(
	maxAttempts int,
	window time.Duration,
	now func() time.Time,
) (*FixedWindowLoginLimiter, error) {
	if maxAttempts <= 0 || window <= 0 || now == nil {
		return nil, ErrInvalidLoginRateLimit
	}
	currentTime := now().UTC()
	return &FixedWindowLoginLimiter{
		entries:     make(map[string]loginLimitEntry),
		maxAttempts: maxAttempts,
		window:      window,
		now:         now,
		lastCleanup: currentTime,
	}, nil
}

func (l *FixedWindowLoginLimiter) Allow(username, clientID string) (bool, time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := l.now().UTC()
	l.cleanupExpired(now)

	keys := []string{
		"username:" + strings.ToLower(strings.TrimSpace(username)),
		"client:" + strings.TrimSpace(clientID),
	}

	var retryAfter time.Duration
	for _, key := range keys {
		entry, exists := l.entries[key]
		if !exists || !now.Before(entry.resetAt) {
			continue
		}
		if entry.attempts >= l.maxAttempts {
			remaining := entry.resetAt.Sub(now)
			if remaining > retryAfter {
				retryAfter = remaining
			}
		}
	}
	if retryAfter > 0 {
		return false, retryAfter
	}

	for _, key := range keys {
		entry, exists := l.entries[key]
		if !exists || !now.Before(entry.resetAt) {
			entry = loginLimitEntry{resetAt: now.Add(l.window)}
		}
		entry.attempts++
		l.entries[key] = entry
	}
	return true, 0
}

func (l *FixedWindowLoginLimiter) cleanupExpired(now time.Time) {
	if now.Sub(l.lastCleanup) < l.window {
		return
	}
	for key, entry := range l.entries {
		if !now.Before(entry.resetAt) {
			delete(l.entries, key)
		}
	}
	l.lastCleanup = now
}
