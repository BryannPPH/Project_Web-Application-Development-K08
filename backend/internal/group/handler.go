package group

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
)

// Handler mengekspos endpoint admin group, membership, dan grant.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	read := func(handler http.HandlerFunc) http.Handler { return security.RequireAuthenticated(handler) }
	write := func(handler http.HandlerFunc) http.Handler { return security.ProtectMutation(handler) }

	mux.Handle("GET /api/v1/admin/groups", read(h.list))
	mux.Handle("POST /api/v1/admin/groups", write(h.create))
	mux.Handle("GET /api/v1/admin/groups/{id}", read(h.get))
	mux.Handle("PATCH /api/v1/admin/groups/{id}", write(h.update))
	mux.Handle("DELETE /api/v1/admin/groups/{id}", write(h.delete))

	mux.Handle("GET /api/v1/admin/groups/{id}/members", read(h.listMembers))
	mux.Handle("POST /api/v1/admin/groups/{id}/members/{userId}", write(h.addMember))
	mux.Handle("DELETE /api/v1/admin/groups/{id}/members/{userId}", write(h.removeMember))

	mux.Handle("GET /api/v1/admin/groups/{id}/databases", read(h.listGroupGrants))
	mux.Handle("PUT /api/v1/admin/groups/{id}/databases/{databaseId}", write(h.upsertGrant))
	mux.Handle("DELETE /api/v1/admin/groups/{id}/databases/{databaseId}", write(h.deleteGrant))
	mux.Handle("GET /api/v1/admin/databases/{id}/grants", read(h.listDatabaseGrants))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	groups, err := h.service.List(r.Context(), principal)
	if err != nil {
		h.writeError(w, r, "list groups", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	record, err := h.service.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "get group", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"group": record})
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
		h.writeError(w, r, "create group", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusCreated, map[string]any{"group": record})
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
		h.writeError(w, r, "update group", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"group": record})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), principal, r.PathValue("id")); err != nil {
		h.writeError(w, r, "delete group", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	members, err := h.service.ListMembers(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "list group members", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	added, err := h.service.AddMember(r.Context(), principal, r.PathValue("id"), r.PathValue("userId"))
	if err != nil {
		h.writeError(w, r, "add group member", err)
		return
	}
	status := http.StatusOK
	if added {
		status = http.StatusCreated
	}
	apihttp.WriteJSON(w, status, map[string]any{"added": added})
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	removed, err := h.service.RemoveMember(r.Context(), principal, r.PathValue("id"), r.PathValue("userId"))
	if err != nil {
		h.writeError(w, r, "remove group member", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

func (h *Handler) listGroupGrants(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	grants, err := h.service.ListGrantsByGroup(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "list group grants", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *Handler) listDatabaseGrants(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	grants, err := h.service.ListGrantsByDatabase(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, "list database grants", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *Handler) upsertGrant(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var input GrantInput
	if err := apihttp.DecodeJSON(w, r, &input); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	grant, err := h.service.UpsertGrant(r.Context(), principal, r.PathValue("id"), r.PathValue("databaseId"), input)
	if err != nil {
		h.writeError(w, r, "upsert grant", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"grant": grant})
}

func (h *Handler) deleteGrant(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteGrant(r.Context(), principal, r.PathValue("id"), r.PathValue("databaseId")); err != nil {
		h.writeError(w, r, "delete grant", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		apihttp.WriteError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "group not found")
	case errors.Is(err, ErrUserNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
	case errors.Is(err, ErrDatabaseNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "DATABASE_NOT_FOUND", "database not found")
	case errors.Is(err, ErrGrantNotFound):
		apihttp.WriteError(w, http.StatusNotFound, "GRANT_NOT_FOUND", "grant not found")
	case errors.Is(err, ErrDuplicateName):
		apihttp.WriteError(w, http.StatusConflict, "GROUP_NAME_TAKEN", "group name already exists")
	default:
		apihttp.InternalError(w, r, h.logger, operation, err)
	}
}
