package migration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
)

type handlerServiceStub struct {
	createEntry    Entry
	createErr      error
	createCalls    int
	reconcileEntry Entry
	reconcileErr   error
	reconcileCalls int
}

func (s *handlerServiceStub) CreateDraft(context.Context, auth.Principal, string, string, string) (Entry, error) {
	s.createCalls++
	return s.createEntry, s.createErr
}
func (*handlerServiceStub) UpdateDraft(context.Context, auth.Principal, string, string, string, string) (Entry, error) {
	return Entry{}, nil
}
func (*handlerServiceStub) List(context.Context, auth.Principal, string, ListOptions) (Page, error) {
	return Page{}, nil
}
func (*handlerServiceStub) Get(context.Context, auth.Principal, string, string) (Entry, error) {
	return Entry{}, nil
}
func (*handlerServiceStub) Execute(context.Context, auth.Principal, string, string) (Entry, error) {
	return Entry{}, nil
}
func (s *handlerServiceStub) Reconcile(context.Context, auth.Principal, string, string, string) (Entry, error) {
	s.reconcileCalls++
	return s.reconcileEntry, s.reconcileErr
}

func TestCreateHandlerReturnsCreatedDraft(t *testing.T) {
	t.Parallel()
	stub := &handlerServiceStub{createEntry: Entry{ID: "created", Status: StatusDraft}}
	handler := NewHandler(stub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"widgets","sql":"SELECT 1"}`))
	request.SetPathValue("databaseId", "8d728274-b56e-4f93-8893-5b02d76a58cb")
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "user"}))
	recorder := httptest.NewRecorder()

	handler.create(recorder, request)

	if recorder.Code != http.StatusCreated || !strings.Contains(recorder.Body.String(), `"id":"created"`) {
		t.Fatalf("response status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestCreateHandlerRejectsUnknownFieldsAndMultipleJSONValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"name":"widgets","sql":"SELECT 1","extra":true}`},
		{name: "multiple objects", body: `{"name":"widgets","sql":"SELECT 1"} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stub := &handlerServiceStub{}
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.SetPathValue("databaseId", "8d728274-b56e-4f93-8893-5b02d76a58cb")
			request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "user"}))
			recorder := httptest.NewRecorder()
			recorder.Header().Set("X-Request-ID", "json-validation-test")

			NewHandler(stub, slog.New(slog.NewTextHandler(io.Discard, nil))).create(recorder, request)

			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"INVALID_JSON"`) {
				t.Fatalf("response status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"requestId":"json-validation-test"`) {
				t.Fatalf("response did not preserve requestId: %s", recorder.Body.String())
			}
			if stub.createCalls != 0 {
				t.Fatalf("service called %d times for invalid JSON", stub.createCalls)
			}
		})
	}
}

func TestCreateHandlerRejectsInvalidIdentifierBeforeService(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&handlerServiceStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"widgets","sql":"SELECT 1"}`))
	request.SetPathValue("databaseId", "not-a-uuid")
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "user"}))
	recorder := httptest.NewRecorder()

	handler.create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReconcileHandlerRequiresInspectionConfirmation(t *testing.T) {
	t.Parallel()
	stub := &handlerServiceStub{}
	handler := NewHandler(stub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"inspectedTarget":false,"note":"checked table state"}`))
	request.SetPathValue("databaseId", "8d728274-b56e-4f93-8893-5b02d76a58cb")
	request.SetPathValue("migrationId", "8d728274-b56e-4f93-8893-5b02d76a58cb")
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "user"}))
	recorder := httptest.NewRecorder()
	handler.reconcile(recorder, request)
	if recorder.Code != http.StatusBadRequest || stub.reconcileCalls != 0 {
		t.Fatalf("status=%d service calls=%d body=%s", recorder.Code, stub.reconcileCalls, recorder.Body.String())
	}
}

func TestReconcileHandlerReturnsRecordedEntry(t *testing.T) {
	t.Parallel()
	stub := &handlerServiceStub{reconcileEntry: Entry{ID: "reconciled", Status: StatusReconciled}}
	handler := NewHandler(stub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"inspectedTarget":true,"note":"Verified target schema manually."}`))
	request.SetPathValue("databaseId", "8d728274-b56e-4f93-8893-5b02d76a58cb")
	request.SetPathValue("migrationId", "8d728274-b56e-4f93-8893-5b02d76a58cb")
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "user"}))
	recorder := httptest.NewRecorder()
	handler.reconcile(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"reconciled"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHandlerMapsAuthorizationAndConflictErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{authz.ErrUnauthenticated, http.StatusUnauthorized, "UNAUTHENTICATED"},
		{authz.ErrDatabaseNotFound, http.StatusNotFound, "DATABASE_NOT_FOUND"},
		{authz.ErrDatabaseInactive, http.StatusConflict, "DATABASE_INACTIVE"},
		{authz.ErrDatabaseEditForbidden, http.StatusForbidden, "DATABASE_EDIT_FORBIDDEN"},
		{ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{ErrConflict, http.StatusConflict, "MIGRATION_CONFLICT"},
		{ErrSQLPolicy, http.StatusUnprocessableEntity, "SQL_POLICY_VIOLATION"},
		{errors.New("unexpected"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		recorder.Header().Set("X-Request-ID", "migration-request")
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		NewHandler(&handlerServiceStub{}, slog.New(slog.NewTextHandler(io.Discard, nil))).writeServiceError(recorder, request, test.err)
		if recorder.Code != test.status {
			t.Fatalf("error %v status=%d want=%d", test.err, recorder.Code, test.status)
		}
		if !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) ||
			!strings.Contains(recorder.Body.String(), `"requestId":"migration-request"`) {
			t.Fatalf("error %v body=%s", test.err, recorder.Body.String())
		}
	}
}
