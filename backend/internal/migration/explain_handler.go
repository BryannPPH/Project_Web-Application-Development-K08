package migration

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
)

// Explainer adalah kontrak service analisis plan yang dipakai handler.
type Explainer interface {
	Explain(context.Context, auth.Principal, string, string, bool) (ExplainResult, error)
}

// ExplainHandler melayani POST /api/v1/databases/{databaseId}/migrations/explain.
type ExplainHandler struct {
	service Explainer
	errors  *Handler
}

func NewExplainHandler(service Explainer, logger *slog.Logger) *ExplainHandler {
	// Pemetaan error dipinjam dari Handler migration agar kode error konsisten.
	return &ExplainHandler{service: service, errors: NewHandler(nil, logger)}
}

func (h *ExplainHandler) RegisterRoutes(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/explain", security.ProtectMutation(http.HandlerFunc(h.explain)))
}

type explainRequest struct {
	SQL     string `json:"sql"`
	Analyze bool   `json:"analyze"`
}

func (h *ExplainHandler) explain(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	var request explainRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := h.service.Explain(r.Context(), principal, databaseID, request.SQL, request.Analyze)
	if err != nil {
		h.errors.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, result)
}
