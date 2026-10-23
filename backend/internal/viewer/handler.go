package viewer

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// Handler mengekspos endpoint viewer read-only.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Register mendaftarkan route viewer. Semua GET; minimum access view_only dicek di service.
//
// Keputusan desain: detail table (columns + constraints + indexes) digabung
// dalam satu endpoint, sedangkan data rows dipisah karena terpaginasi.
func (h *Handler) Register(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	read := func(handler http.HandlerFunc) http.Handler { return security.RequireAuthenticated(handler) }

	mux.Handle("GET /api/v1/databases/{id}/schemas", read(h.schemas))
	mux.Handle("GET /api/v1/databases/{id}/schemas/{schema}/tables", read(h.tables))
	mux.Handle("GET /api/v1/databases/{id}/schemas/{schema}/tables/{table}", read(h.tableDetail))
	mux.Handle("GET /api/v1/databases/{id}/schemas/{schema}/tables/{table}/rows", read(h.rows))
}

func (h *Handler) schemas(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	databaseID := r.PathValue("id")
	schemas, err := h.service.Schemas(r.Context(), principal, databaseID)
	if err != nil {
		h.writeError(w, r, databaseID, err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"schemas": schemas})
}

func (h *Handler) tables(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	databaseID := r.PathValue("id")
	tables, err := h.service.Tables(r.Context(), principal, databaseID, r.PathValue("schema"))
	if err != nil {
		h.writeError(w, r, databaseID, err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

func (h *Handler) tableDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	databaseID := r.PathValue("id")
	detail, err := h.service.TableDetail(r.Context(), principal, databaseID, r.PathValue("schema"), r.PathValue("table"))
	if err != nil {
		h.writeError(w, r, databaseID, err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, detail)
}

func (h *Handler) rows(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	page, err := ParsePage(query.Get("limit"), query.Get("offset"))
	if err != nil {
		h.writeError(w, r, "", err)
		return
	}
	databaseID := r.PathValue("id")
	result, err := h.service.Rows(r.Context(), principal, databaseID, r.PathValue("schema"), r.PathValue("table"), page)
	if err != nil {
		h.writeError(w, r, databaseID, err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, result)
}

func requirePrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		apihttp.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
	}
	return principal, ok
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, databaseID string, err error) {
	if public, ok := authz.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	switch {
	case errors.Is(err, ErrInvalidPagination):
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_PAGINATION",
			"limit must be between 1 and 100 and offset must be zero or greater")
		return
	case errors.Is(err, ErrSchemaNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "SCHEMA_NOT_FOUND", "schema not found")
		return
	case errors.Is(err, ErrTableNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "TABLE_NOT_FOUND", "table not found")
		return
	}

	// Kegagalan target: log ID database + request ID, kirim pesan generik.
	if h.logger != nil {
		h.logger.WarnContext(r.Context(), "viewer target operation failed",
			"request_id", apihttp.RequestID(w),
			"database_id", databaseID,
			"error", err.Error(),
		)
	}
	switch {
	case errors.Is(err, ErrTargetPermissionDenied):
		apihttp.WriteError(w, http.StatusForbidden, "TARGET_PERMISSION_DENIED",
			"the platform connection role cannot read this object")
	case errors.Is(err, ErrTargetTimeout):
		apihttp.WriteError(w, http.StatusGatewayTimeout, "TARGET_TIMEOUT", "target database query timed out")
	case errors.Is(err, ErrTargetQueryFailed):
		apihttp.WriteError(w, http.StatusBadGateway, "TARGET_QUERY_FAILED", "target database query failed")
	default:
		if public, ok := targetdb.Describe(err); ok {
			apihttp.WriteError(w, public.Status, public.Code, public.Message)
			return
		}
		apihttp.InternalError(w, r, h.logger, "viewer operation", err)
	}
}
