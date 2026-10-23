package user

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
)

// Handler mengekspos endpoint admin user.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	mux.Handle("GET /api/v1/admin/users", security.RequireAuthenticated(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/admin/users", security.ProtectMutation(http.HandlerFunc(h.create)))
	mux.Handle("GET /api/v1/admin/users/{id}", security.RequireAuthenticated(http.HandlerFunc(h.get)))
	mux.Handle("PATCH /api/v1/admin/users/{id}", security.ProtectMutation(http.HandlerFunc(h.update)))
	mux.Handle("POST /api/v1/admin/users/{id}/reset-password", security.ProtectMutation(http.HandlerFunc(h.resetPassword)))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	users, err := h.service.List(r.Context(), principal)
	if err != nil {
		h.writeError(w, r, "list users", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	record, err := h.service.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "get user", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"user": record})
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
	record, err := h.service.Create(r.Context(), principal, input)
	if err != nil {
		h.writeError(w, r, "create user", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusCreated, map[string]any{"user": record})
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
	record, err := h.service.Update(r.Context(), principal, r.PathValue("id"), input)
	if err != nil {
		h.writeError(w, r, "update user", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"user": record})
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var input ResetPasswordInput
	if err := apihttp.DecodeJSON(w, r, &input); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	record, err := h.service.ResetPassword(r.Context(), principal, r.PathValue("id"), input)
	if err != nil {
		h.writeError(w, r, "reset user password", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"user": record})
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
		apihttp.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
	case errors.Is(err, ErrDuplicateUsername):
		apihttp.WriteError(w, http.StatusConflict, "USERNAME_TAKEN", "username already exists")
	case errors.Is(err, ErrLastActiveAdmin):
		apihttp.WriteError(w, http.StatusConflict, "LAST_ACTIVE_ADMIN", "the last active platform admin cannot be demoted or deactivated")
	default:
		apihttp.InternalError(w, r, h.logger, operation, err)
	}
}
