package database

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

// Handler mengekspos endpoint admin managed database dan daftar database user.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Register mendaftarkan route. GET memakai RequireAuthenticated; mutation memakai
// ProtectMutation (origin + session + CSRF). Policy admin dicek di service.
func (h *Handler) Register(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	read := func(handler http.HandlerFunc) http.Handler { return security.RequireAuthenticated(handler) }
	write := func(handler http.HandlerFunc) http.Handler { return security.ProtectMutation(handler) }

	mux.Handle("GET /api/v1/admin/databases", read(h.list))
	mux.Handle("POST /api/v1/admin/databases", write(h.create))
	mux.Handle("POST /api/v1/admin/databases/test-connection", write(h.testDraft))
	mux.Handle("GET /api/v1/admin/databases/{id}", read(h.get))
	mux.Handle("PATCH /api/v1/admin/databases/{id}", write(h.update))
	mux.Handle("DELETE /api/v1/admin/databases/{id}", write(h.delete))
	mux.Handle("POST /api/v1/admin/databases/{id}/test", write(h.testStored))

	mux.Handle("GET /api/v1/databases", read(h.listAccessible))
	mux.Handle("GET /api/v1/databases/{id}", read(h.getAccessible))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	databases, err := h.service.List(r.Context(), principal)
	if err != nil {
		h.writeError(w, r, "list managed databases", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"databases": databases})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	database, err := h.service.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "get managed database", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"database": database})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var input CreateInput
	if err := apihttp.DecodeJSON(w, r, &input); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	database, err := h.service.Create(r.Context(), principal, input)
	if err != nil {
		h.writeError(w, r, "create managed database", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusCreated, map[string]any{"database": database})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var input UpdateInput
	if err := apihttp.DecodeJSON(w, r, &input); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	database, err := h.service.Update(r.Context(), principal, r.PathValue("id"), input)
	if err != nil {
		h.writeError(w, r, "update managed database", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"database": database})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), principal, r.PathValue("id")); err != nil {
		h.writeError(w, r, "delete managed database", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) testStored(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	result, err := h.service.TestStored(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "test stored managed database", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) testDraft(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var input CreateInput
	if err := apihttp.DecodeJSON(w, r, &input); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	result, err := h.service.TestDraft(r.Context(), principal, input)
	if err != nil {
		h.writeError(w, r, "test draft managed database", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) listAccessible(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	databases, err := h.service.ListAccessible(r.Context(), principal)
	if err != nil {
		h.writeError(w, r, "list accessible databases", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"databases": databases})
}

func (h *Handler) getAccessible(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	database, err := h.service.GetAccessible(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "get accessible database", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"database": database})
}

func requirePrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		apihttp.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
	}
	return principal, ok
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if public, ok := authz.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		apihttp.WriteFieldError(w, validation.Field, validation.Message)
	case errors.Is(err, ErrNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "DATABASE_NOT_FOUND", "database not found")
	case errors.Is(err, ErrDuplicateName):
		apihttp.WriteError(w, http.StatusConflict, "DATABASE_NAME_TAKEN", "database name already exists")
	case errors.Is(err, ErrInUse):
		apihttp.WriteError(w, http.StatusConflict, "DATABASE_HAS_MIGRATIONS",
			"database has migration history; deactivate it instead")
	default:
		if public, ok := targetdb.Describe(err); ok {
			apihttp.WriteError(w, public.Status, public.Code, public.Message)
			return
		}
		apihttp.InternalError(w, r, h.logger, operation, err)
	}
}
