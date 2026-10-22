package session

import (
	"net/http"
	"time"
)

type CookieConfig struct {
	Name   string
	Secure bool
}

// NewCookie membuat cookie sesi yang tidak dapat diakses JavaScript.
func (c CookieConfig) NewCookie(token string, absoluteExpiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(absoluteExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	return &http.Cookie{
		Name:     c.Name,
		Value:    token,
		Path:     "/",
		Expires:  absoluteExpiresAt.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (c CookieConfig) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     c.Name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}
