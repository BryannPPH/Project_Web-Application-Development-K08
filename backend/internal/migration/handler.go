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

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/approval"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/apihttp"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type MigrationService interface {
	CreateDraft(context.Context, auth.Principal, string, string, string) (Entry, error)
	UpdateDraft(context.Context, auth.Principal, string, string, string, string) (Entry, error)
	List(context.Context, auth.Principal, string, ListOptions) (Page, error)
	Get(context.Context, auth.Principal, string, string) (Entry, error)
	Execute(context.Context, auth.Principal, string, string) (Entry, error)
	Reconcile(context.Context, auth.Principal, string, string, string) (Entry, error)
}

type DefinitionMigrationService interface {
	CreateDraftWithDefinition(context.Context, auth.Principal, string, Definition) (Entry, error)
	UpdateDraftWithDefinition(context.Context, auth.Principal, string, string, Definition) (Entry, error)
}

type ExecutionActionService interface {
	Retry(context.Context, auth.Principal, string, string) (Entry, error)
	Rollback(context.Context, auth.Principal, string, string) (Entry, error)
	Forward(context.Context, auth.Principal, string, string) (Entry, error)
}

type HistoryMigrationService interface {
	ListHistory(context.Context, auth.Principal, string, HistoryOptions) (HistoryPage, error)
	GetExecution(context.Context, auth.Principal, string, string) (ExecutionAttempt, error)
	GetBatchProgress(context.Context, auth.Principal, string, string) (BatchProgress, error)
}

type JobHandlerService interface {
	EnqueueAction(context.Context, auth.Principal, string, string, ExecutionAction, EnqueueMetadata) (Job, error)
	GetJob(context.Context, auth.Principal, string, string) (Job, error)
	LatestJobID(context.Context, auth.Principal, string, string) (*string, error)
}

type Handler struct {
	service             MigrationService
	jobs                JobHandlerService
	logger              *slog.Logger
	maxSQLJSONBodyBytes int64
}

func NewHandler(service MigrationService, logger *slog.Logger) *Handler {
	return newHandler(service, logger, DefaultMaxSQLBytes)
}

func NewHandlerWithJobs(service MigrationService, jobs JobHandlerService, logger *slog.Logger) *Handler {
	return newHandlerWithJobs(service, jobs, logger, DefaultMaxSQLBytes)
}

func newHandler(service MigrationService, logger *slog.Logger, maxSQLBytes int) *Handler {
	return newHandlerWithJobs(service, nil, logger, maxSQLBytes)
}

func newHandlerWithJobs(service MigrationService, jobs JobHandlerService, logger *slog.Logger, maxSQLBytes int) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service:             service,
		jobs:                jobs,
		logger:              logger,
		maxSQLJSONBodyBytes: maxMigrationJSONBodyBytes(maxSQLBytes),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
	read := security.RequireAuthenticated
	mutate := security.ProtectMutation
	mux.Handle("GET /api/v1/databases/{databaseId}/migrations", read(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations", mutate(http.HandlerFunc(h.create)))
	mux.Handle("GET /api/v1/databases/{databaseId}/migrations/{migrationId}", read(http.HandlerFunc(h.detail)))
	mux.Handle("PATCH /api/v1/databases/{databaseId}/migrations/{migrationId}", mutate(http.HandlerFunc(h.update)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/execute", mutate(http.HandlerFunc(h.execute)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/retry", mutate(http.HandlerFunc(h.retry)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/rollback", mutate(http.HandlerFunc(h.rollback)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/forward", mutate(http.HandlerFunc(h.forward)))
	mux.Handle("POST /api/v1/databases/{databaseId}/migrations/{migrationId}/reconcile", mutate(http.HandlerFunc(h.reconcile)))
	mux.Handle("GET /api/v1/databases/{databaseId}/migration-history", read(http.HandlerFunc(h.history)))
	mux.Handle("GET /api/v1/databases/{databaseId}/migration-executions/{executionId}", read(http.HandlerFunc(h.executionDetail)))
	mux.Handle("GET /api/v1/databases/{databaseId}/migration-executions/{executionId}/progress", read(http.HandlerFunc(h.executionProgress)))
	mux.Handle("GET /api/v1/databases/{databaseId}/migration-jobs/{jobId}", read(http.HandlerFunc(h.jobStatus)))
}

type draftRequest struct {
	Name               string        `json:"name"`
	SQL                string        `json:"sql"`
	DownSQL            *string       `json:"downSql"`
	IrreversibleReason *string       `json:"irreversibleReason"`
	ExecutionMode      ExecutionMode `json:"executionMode"`
	BatchSelectSQL     *string       `json:"batchSelectSql"`
	BatchSize          *int          `json:"batchSize"`
	BatchCursorStart   int64         `json:"batchCursorStart"`
}

type reconcileRequest struct {
	InspectedTarget bool   `json:"inspectedTarget"`
	Note            string `json:"note"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	var request draftRequest
	if !decodeJSON(w, r, &request, h.maxSQLJSONBodyBytes) {
		return
	}
	var entry Entry
	var err error
	if definitionService, ok := h.service.(DefinitionMigrationService); ok {
		entry, err = definitionService.CreateDraftWithDefinition(r.Context(), principal, databaseID, request.definition())
	} else {
		entry, err = h.service.CreateDraft(r.Context(), principal, databaseID, request.Name, request.SQL)
	}
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
	if !decodeJSON(w, r, &request, h.maxSQLJSONBodyBytes) {
		return
	}
	var entry Entry
	var err error
	if definitionService, ok := h.service.(DefinitionMigrationService); ok && request.hasDefinition() {
		entry, err = definitionService.UpdateDraftWithDefinition(r.Context(), principal, databaseID, r.PathValue("migrationId"), request.definition())
	} else {
		entry, err = h.service.UpdateDraft(r.Context(), principal, databaseID, r.PathValue("migrationId"), request.Name, request.SQL)
	}
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, entry)
}

func (request draftRequest) definition() Definition {
	return Definition{Name: request.Name, SQL: request.SQL, DownSQL: request.DownSQL,
		IrreversibleReason: request.IrreversibleReason, ExecutionMode: request.ExecutionMode,
		BatchSelectSQL: request.BatchSelectSQL, BatchSize: request.BatchSize,
		BatchCursorStart: request.BatchCursorStart}
}

func (request draftRequest) hasDefinition() bool {
	return request.DownSQL != nil || request.IrreversibleReason != nil || request.ExecutionMode != "" ||
		request.BatchSelectSQL != nil || request.BatchSize != nil || request.BatchCursorStart != 0
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
	migrationID := r.PathValue("migrationId")
	entry, err := h.service.Get(r.Context(), principal, databaseID, migrationID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if h.jobs != nil {
		entry.LatestJobID, err = h.jobs.LatestJobID(r.Context(), principal, databaseID, migrationID)
		if err != nil {
			h.writeServiceError(w, r, err)
			return
		}
	}
	writeMigrationJSON(w, http.StatusOK, entry)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	h.runAction(w, r, ActionApply)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	h.runAction(w, r, ActionRetry)
}

func (h *Handler) rollback(w http.ResponseWriter, r *http.Request) {
	h.runAction(w, r, ActionRollback)
}

func (h *Handler) forward(w http.ResponseWriter, r *http.Request) {
	h.runAction(w, r, ActionForward)
}

func (h *Handler) runAction(w http.ResponseWriter, r *http.Request, action ExecutionAction) {
	databaseID, principal, ok := requestIdentity(w, r, true)
	if !ok {
		return
	}
	if h.jobs == nil {
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration action is unavailable")
		return
	}
	traceparent, tracestate := observability.StoredTraceContext(r.Context())
	job, err := h.jobs.EnqueueAction(r.Context(), principal, databaseID, r.PathValue("migrationId"), action, EnqueueMetadata{
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		CorrelationID:  httpapi.RequestIDFromContext(r.Context()),
		Traceparent:    traceparent,
		Tracestate:     tracestate,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	location := "/api/v1/databases/" + databaseID + "/migration-jobs/" + job.ID
	w.Header().Set("Location", location)
	writeMigrationJSON(w, http.StatusAccepted, job)
}

func (h *Handler) jobStatus(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	jobID := r.PathValue("jobId")
	if !uuidPattern.MatchString(jobID) {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid job identifier")
		return
	}
	if h.jobs == nil {
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration jobs are unavailable")
		return
	}
	job, err := h.jobs.GetJob(r.Context(), principal, databaseID, jobID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, job)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	service, ok := h.service.(HistoryMigrationService)
	if !ok {
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration history is unavailable")
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
	executionID := strings.TrimSpace(r.URL.Query().Get("executionId"))
	if executionID != "" && !uuidPattern.MatchString(executionID) {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid execution identifier")
		return
	}
	hideRollback := false
	if value := strings.TrimSpace(r.URL.Query().Get("hideRollback")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid history filter")
			return
		}
		hideRollback = parsed
	}
	page, err := service.ListHistory(r.Context(), principal, databaseID, HistoryOptions{
		Limit: limit, Offset: offset, ExecutionID: executionID, HideRollback: hideRollback,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, page)
}

func (h *Handler) executionDetail(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	executionID := r.PathValue("executionId")
	if !uuidPattern.MatchString(executionID) {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid execution identifier")
		return
	}
	service, ok := h.service.(HistoryMigrationService)
	if !ok {
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration history is unavailable")
		return
	}
	attempt, err := service.GetExecution(r.Context(), principal, databaseID, executionID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, attempt)
}

func (h *Handler) executionProgress(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, false)
	if !ok {
		return
	}
	executionID := r.PathValue("executionId")
	if !uuidPattern.MatchString(executionID) {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid execution identifier")
		return
	}
	service, ok := h.service.(HistoryMigrationService)
	if !ok {
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration progress is unavailable")
		return
	}
	progress, err := service.GetBatchProgress(r.Context(), principal, databaseID, executionID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeMigrationJSON(w, http.StatusOK, progress)
}

func (h *Handler) reconcile(w http.ResponseWriter, r *http.Request) {
	databaseID, principal, ok := requestIdentity(w, r, true)
	if !ok {
		return
	}
	var request reconcileRequest
	if !decodeJSON(w, r, &request, apihttp.MaxJSONBodyBytes) {
		return
	}
	if !request.InspectedTarget {
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "target inspection must be confirmed")
		return
	}
	entry, err := h.service.Reconcile(r.Context(), principal, databaseID, r.PathValue("migrationId"), request.Note)
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

const (
	maxJSONByteExpansion = int64(6)
	jsonMetadataBytes    = int64(4 * 1024)
)

func maxMigrationJSONBodyBytes(maxSQLBytes int) int64 {
	if maxSQLBytes <= 0 {
		maxSQLBytes = DefaultMaxSQLBytes
	}
	return int64(maxSQLBytes)*maxJSONByteExpansion + jsonMetadataBytes
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBodyBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if isRequestTooLarge(err) {
			writeMigrationError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body is too large")
			return false
		}
		writeMigrationError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if isRequestTooLarge(err) {
			writeMigrationError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body is too large")
			return false
		}
		writeMigrationError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
		return false
	}
	return true
}

func isRequestTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if public, ok := authz.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	if public, ok := approval.Describe(err); ok {
		apihttp.WriteError(w, public.Status, public.Code, public.Message)
		return
	}
	switch {
	case errors.Is(err, ErrRevisionChanged):
		writeMigrationError(w, http.StatusConflict, "MIGRATION_REVISION_CHANGED", "migration was edited during the request; review and approve the new revision")
	case errors.Is(err, ErrNotFound):
		writeMigrationError(w, http.StatusNotFound, "NOT_FOUND", "migration not found")
	case errors.Is(err, ErrConflict):
		writeMigrationError(w, http.StatusConflict, "MIGRATION_CONFLICT", "migration is no longer an editable draft")
	case errors.Is(err, ErrTargetBatchBlocked):
		code, message := publicJobError(err)
		writeMigrationError(w, http.StatusConflict, code, message)
	case errors.Is(err, ErrWrongOrder):
		writeMigrationError(w, http.StatusConflict, "MIGRATION_ORDER_BLOCKED", "an earlier migration blocks execution")
	case errors.Is(err, ErrIrreversible):
		writeMigrationError(w, http.StatusConflict, "MIGRATION_IRREVERSIBLE", "migration is marked irreversible and cannot be rolled back")
	case errors.Is(err, ErrNoAttempt):
		writeMigrationError(w, http.StatusNotFound, "EXECUTION_NOT_FOUND", "migration execution not found")
	case errors.Is(err, ErrInvalidInput):
		writeMigrationError(w, http.StatusBadRequest, "INVALID_INPUT", "migration name or SQL is invalid")
	case errors.Is(err, ErrSQLPolicy):
		writeMigrationError(w, http.StatusUnprocessableEntity, "SQL_POLICY_VIOLATION", "SQL is not allowed by the target migration policy")
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
