package schemadiff

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

const maxRequestBytes = 8 * 1024

type Comparer interface {
	Compare(context.Context, auth.Principal, Request) (Result, error)
}

type Handler struct {
	service Comparer
	logger  *slog.Logger
}

func NewHandler(service Comparer, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// RegisterRoutes: POST karena request membawa body; tetap dilindungi CSRF.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	mux.Handle("POST /api/v1/schema-diff", security.ProtectMutation(http.HandlerFunc(h.compare)))
}

func (h *Handler) compare(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		apihttp.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
		return
	}

	result, err := h.service.Compare(r.Context(), principal, request)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if public, ok := authz.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	switch {
	case errors.Is(err, ErrInvalidInput):
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_INPUT", "source and target must name a database and schema")
	case errors.Is(err, ErrSchemaNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "SCHEMA_NOT_FOUND", "schema not found")
	case errors.Is(err, ErrEngineMismatch):
		apihttp.WriteError(w, http.StatusUnprocessableEntity, "ENGINE_MISMATCH", "source and target must use the same database engine")
	case errors.Is(err, context.DeadlineExceeded):
		apihttp.WriteError(w, http.StatusGatewayTimeout, "TARGET_TIMEOUT", "target query timed out")
	default:
		if public, ok := targetdb.Describe(err); ok {
			apihttp.WriteError(w, public.Status, public.Code, public.Message)
			return
		}
		h.logger.ErrorContext(r.Context(), "schema diff failed", "error", err)
		apihttp.WriteError(w, http.StatusBadGateway, "TARGET_QUERY_FAILED", "target catalog query failed")
	}
}
