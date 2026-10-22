package session

import (
	"net/http"
	"testing"
	"time"
)

func TestSessionCookieSecurityAttributes(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(time.Hour)
	cookie := (CookieConfig{Name: "dbmp_session", Secure: true}).NewCookie("opaque-token", expiresAt)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie security attributes are incomplete: %+v", cookie)
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("cookie MaxAge = %d, want positive", cookie.MaxAge)
	}

	cleared := (CookieConfig{Name: "dbmp_session", Secure: true}).ClearCookie()
	if cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.HttpOnly || !cleared.Secure {
		t.Fatalf("clear cookie attributes are incorrect: %+v", cleared)
	}
}
