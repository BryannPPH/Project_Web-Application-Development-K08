package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func newTestRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "dbmp_test_total", Help: "test"})
	counter.Inc()
	registry.MustRegister(counter)
	return registry
}

func scrape(handler http.Handler, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestHandlerWithoutTokenIsOpen(t *testing.T) {
	recorder := scrape(Handler(newTestRegistry(), ""), "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "dbmp_test_total 1") {
		t.Fatalf("scrape = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerRequiresBearerTokenWhenConfigured(t *testing.T) {
	handler := Handler(newTestRegistry(), "rahasia")
	for _, header := range []string{"", "Bearer salah", "rahasia", "Basic rahasia"} {
		if recorder := scrape(handler, header); recorder.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q = %d, want 401", header, recorder.Code)
		}
	}
	if recorder := scrape(handler, "Bearer rahasia"); recorder.Code != http.StatusOK {
		t.Fatalf("valid token = %d", recorder.Code)
	}
}

func TestDurationQueryHasOneColumnPerBucket(t *testing.T) {
	query := durationQuery()
	if got := strings.Count(query, "FILTER (WHERE d <="); got != len(durationBuckets) {
		t.Fatalf("bucket columns = %d, want %d", got, len(durationBuckets))
	}
}
