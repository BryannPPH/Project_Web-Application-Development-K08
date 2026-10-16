package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	testHandler(nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if requestID := recorder.Header().Get("X-Request-ID"); requestID == "" {
		t.Error("X-Request-ID is empty")
	}

	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status body = %q, want ok", body["status"])
	}
}

func TestNotFoundUsesConsistentErrorResponse(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)

	testHandler(nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var body errorBody
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" {
		t.Errorf("error code = %q, want NOT_FOUND", body.Error.Code)
	}
	if body.Error.RequestID == "" {
		t.Error("error requestId is empty")
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	testHandler(nil).ServeHTTP(recorder, request)

	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := recorder.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestReadiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		databaseErr error
		wantStatus  int
	}{
		{name: "database available", wantStatus: http.StatusOK},
		{name: "database unavailable", databaseErr: errors.New("connection failed"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)

			testHandler(tt.databaseErr).ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
		})
	}
}

type databaseChecker struct {
	err error
}

func (d databaseChecker) Ping(context.Context) error {
	return d.err
}

func testHandler(databaseErr error) http.Handler {
	return NewHandler(Dependencies{
		Logger:                discardLogger(),
		Database:              databaseChecker{err: databaseErr},
		DatabaseHealthTimeout: time.Second,
	})
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
