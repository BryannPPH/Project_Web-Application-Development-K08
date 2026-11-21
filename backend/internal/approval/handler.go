package approval

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
)

// ReviewService: method service yang dipakai handler (interface supaya handler
// bisa diuji dengan service palsu).
type ReviewService interface {
	Review(ctx context.Context, principal auth.Principal, databaseID, migrationID string, action Action) (Review, error)
	Decide(ctx context.Context, principal auth.Principal, databaseID, migrationID string, input DecideInput) (Review, error)
}

// Handler = endpoint approval.
type Handler struct {
	service ReviewService
	logger  *slog.Logger
}

// NewHandler = constructor.
func NewHandler(service ReviewService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// RegisterRoutes: GET/POST /api/v1/databases/{databaseId}/migrations/{migrationId}/approvals.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	mux.Handle("GET /api/v1/databases/{databaseId}/migrations/{migrationId}/approvals",
		security.RequireAuthenticated(http.HandlerFunc(h.review)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/approvals",
		security.ProtectMutation(http.HandlerFunc(h.decide)))
}

// review: GET. Query ?action= boleh kosong -> default "apply".
func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	action := Action(strings.TrimSpace(r.URL.Query().Get("action")))
	if action == "" {
		action = ActionApply
	}
	result, err := h.service.Review(r.Context(), principal, r.PathValue("databaseId"), r.PathValue("migrationId"), action)
	if err != nil {
		h.writeError(w, r, "load approval review", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"review": result})
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var input DecideInput
	if err := apihttp.DecodeJSON(w, r, &input); err != nil {
		apihttp.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	result, err := h.service.Decide(r.Context(), principal, r.PathValue("databaseId"), r.PathValue("migrationId"), input)
	if err != nil {
		h.writeError(w, r, "record approval decision", err)
		return
	}
	apihttp.WriteJSON(w, http.StatusCreated, map[string]any{"review": result})
}

// principalFrom: user yang login (diisi middleware). Tidak ada -> 401.
func principalFrom(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		apihttp.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return auth.Principal{}, false
	}
	return principal, true
}

// writeError: authz dulu (401/403/404 database), lalu error approval,
// sisanya 500 generik (detail hanya di log, tidak bocor ke user).
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if public, ok := authz.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	if public, ok := Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	apihttp.InternalError(w, r, h.logger, operation, err)
}
