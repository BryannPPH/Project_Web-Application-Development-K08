package session

import (
	"net/http"
	"testing"
	"time"
)

func TestSessionCookieSecurityAttributes(t *testing.T) {
	t.Parallel()

	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "development", true: "production"}[secure], func(t *testing.T) {
			expiresAt := time.Now().Add(time.Hour)
			config := CookieConfig{Name: "dbmp_session", Secure: secure}
			cookie := config.NewCookie("opaque-token", expiresAt)
			if !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
				t.Fatalf("cookie security attributes are incomplete: %+v", cookie)
			}
			if cookie.MaxAge <= 0 {
				t.Fatalf("cookie MaxAge = %d, want positive", cookie.MaxAge)
			}

			cleared := config.ClearCookie()
			if cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.HttpOnly || cleared.Secure != secure {
				t.Fatalf("clear cookie attributes are incorrect: %+v", cleared)
			}
		})
	}
}
