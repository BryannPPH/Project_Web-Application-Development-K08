package metrics

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/app"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
)

// Options mengatur endpoint /metrics.
type Options struct {
	// Token opsional. Jika diisi, scrape wajib membawa header
	// "Authorization: Bearer <token>".
	Token string
}

// FeatureFactory mendaftarkan GET /metrics. Endpoint ini tidak memakai session
// login karena di-scrape oleh Prometheus, bukan oleh browser.
func FeatureFactory(options Options) app.FeatureFactory {
	return func(dependencies app.FeatureDependencies) (app.ProtectedRouteRegistrar, error) {
		if dependencies.Database == nil {
			return nil, errors.New("metrics feature requires a database pool")
		}
		registry := prometheus.NewRegistry()
		registry.MustRegister(
			NewCollector(dependencies.Database, dependencies.Logger),
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
		handler := Handler(registry, options.Token)
		return func(mux *http.ServeMux, _ *httpapi.AuthMiddleware) {
			mux.Handle("GET /metrics", handler)
		}, nil
	}
}

// Handler membungkus promhttp dengan pengecekan token opsional.
func Handler(gatherer prometheus.Gatherer, token string) http.Handler {
	metrics := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !validBearer(r.Header.Get("Authorization"), token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		metrics.ServeHTTP(w, r)
	})
}

func validBearer(header, token string) bool {
	given, ok := strings.CutPrefix(header, "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(given), []byte(token)) == 1
}
