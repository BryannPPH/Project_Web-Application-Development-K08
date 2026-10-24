package migration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type MigrationService interface {
	CreateDraft(context.Context, auth.Principal, string, string, string) (Entry, error)
	UpdateDraft(context.Context, auth.Principal, string, string, string, string) (Entry, error)
	List(context.Context, auth.Principal, string, ListOptions) (Page, error)
	Get(context.Context, auth.Principal, string, string) (Entry, error)
	Execute(context.Context, auth.Principal, string, string) (Entry, error)
}

type Handler struct {
	service MigrationService
	logger  *slog.Logger
}

func NewHandler(service MigrationService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	read := security.RequireAuthenticated
	mutate := security.ProtectMutation
	mux.Handle("GET /api/v1/databases/{databaseId}/migrations", read(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations", mutate(http.HandlerFunc(h.create)))
	mux.Handle("GET /api/v1/databases/{databaseId}/migrations/{migrationId}", read(http.HandlerFunc(h.detail)))
	mux.Handle("PATCH /api/v1/databases/{databaseId}/migrations/{migrationId}", mutate(http.HandlerFunc(h.update)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/execute", mutate(http.HandlerFunc(h.execute)))
}

type draftRequest struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	var request draftRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	entry, err := h.service.CreateDraft(r.Context(), principal, databaseID, request.Name, request.SQL)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusCreated, entry)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, true)
	if !ok {
		return
	}
	var request draftRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	entry, err := h.service.UpdateDraft(r.Context(), principal, databaseID, r.PathValue("migrationId"), request.Name, request.SQL)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, entry)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	limit, valid := queryInteger(r, "limit", DefaultPageSize)
	if !valid {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid pagination")
		return
	}
	offset, valid := queryInteger(r, "offset", 0)
	if !valid {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid pagination")
		return
	}
	page, err := h.service.List(r.Context(), principal, databaseID, ListOptions{Limit: limit, Offset: offset})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, page)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, true)
	if !ok {
		return
	}
	entry, err := h.service.Get(r.Context(), principal, databaseID, r.PathValue("migrationId"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, entry)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, true)
	if !ok {
		return
	}
	entry, err := h.service.Execute(r.Context(), principal, databaseID, r.PathValue("migrationId"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, entry)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func requestIdentity(w http.ResponseWriter, r *http.Request, migrationRequired bool) (string, auth.Principal, bool) {
	databaseID := r.PathValue("databaseId")
	if !uuidPattern.MatchString(databaseID) || (migrationRequired && !uuidPattern.MatchString(r.PathValue("migrationId"))) {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid resource identifier")
		return "", auth.Principal{}, false
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		writeMigrationError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return "", auth.Principal{}, false
	}
	return databaseID, principal, true
}

func queryInteger(r *http.Request, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, DefaultMaxSQLBytes+4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
		return false
	}
	return true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if public, ok := authz.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	switch {
	case errors.Is(err, ErrNotFound):
		writeMigrationError(w, http.StatusNotFound, "NOT_FOUND", "migration not found")
	case errors.Is(err, ErrConflict):
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration is no longer an editable draft")
	case errors.Is(err, ErrWrongOrder):
		writeMigrationError(w, http.StatusConflict, "MIGRATION_ORDER_BLOCKED", "an earlier migration blocks execution")
	case errors.Is(err, ErrInvalidInput):
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "migration name or SQL is invalid")
	case errors.Is(err, ErrSQLPolicy):
		writeMigrationError(w, http.StatusUnprocessableEntity, "SQL_POLICY_VIOLATION", "SQL is not allowed by the target transaction policy")
	default:
		if public, ok := targetdb.Describe(err); ok {
			apihttp.WriteError(w, public.Status, public.Code, public.Message)
			return
		}
		h.logger.ErrorContext(r.Context(), "migration request failed", "error", err,
			"database_id", r.PathValue("databaseId"), "migration_id", r.PathValue("migrationId"))
		writeMigrationError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

func writeMigrationError(w http.ResponseWriter, status int, code, message string) {
	apihttp.WriteError(w, status, code, message)
}

func writeMigrationJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
