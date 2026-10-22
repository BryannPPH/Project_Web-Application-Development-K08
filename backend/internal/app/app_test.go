package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
)

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func TestBuildFeatureRegistrarSharesDependenciesAndPreservesOrder(t *testing.T) {
	t.Parallel()

	var configured bool
	var calls []string
	factories := []FeatureFactory{
		func(dependencies FeatureDependencies) (ProtectedRouteRegistrar, error) {
			configured = dependencies.Logger != nil
			return func(*http.ServeMux, *httpapi.AuthMiddleware) {
				calls = append(calls, "first")
			}, nil
		},
		func(FeatureDependencies) (ProtectedRouteRegistrar, error) {
			return func(*http.ServeMux, *httpapi.AuthMiddleware) {
				calls = append(calls, "second")
			}, nil
		},
	}

	registrar, err := buildFeatureRegistrar(factories, FeatureDependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("buildFeatureRegistrar() error = %v", err)
	}
	if !configured || registrar == nil {
		t.Fatal("feature dependencies were not provided")
	}
	registrar(http.NewServeMux(), nil)
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("registrar calls = %v, want [first second]", calls)
	}
}

func TestBuildFeatureRegistrarPropagatesFactoryError(t *testing.T) {
	t.Parallel()

	want := errors.New("feature failed")
	_, err := buildFeatureRegistrar([]FeatureFactory{
		func(FeatureDependencies) (ProtectedRouteRegistrar, error) {
			return nil, want
		},
	}, FeatureDependencies{})
	if !errors.Is(err, want) {
		t.Fatalf("buildFeatureRegistrar() error = %v, want %v", err, want)
	}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return testAddress("test-listener")
}

type testAddress string

func (a testAddress) Network() string { return "test" }
func (a testAddress) String() string  { return string(a) }

func TestRunStopsCleanlyWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	application := &App{
		server: &http.Server{
			Addr:              "127.0.0.1:0",
			Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			ReadHeaderTimeout: time.Second,
		},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		shutdownTimeout: time.Second,
		listen: func(string, string) (net.Listener, error) {
			return &blockingListener{closed: make(chan struct{})}, nil
		},
	}

	if err := application.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}
