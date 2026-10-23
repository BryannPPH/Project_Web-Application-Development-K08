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
)

type handlerServiceStub struct {
	createEntry Entry
	createErr   error
}

func (s *handlerServiceStub) CreateDraft(context.Context, auth.Principal, string, string, string) (Entry, error) {
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

func TestHandlerMapsAuthorizationAndConflictErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
	}{
		{ErrForbidden, http.StatusForbidden},
		{ErrNotFound, http.StatusNotFound},
		{ErrConflict, http.StatusConflict},
		{ErrSQLPolicy, http.StatusUnprocessableEntity},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		NewHandler(&handlerServiceStub{}, slog.New(slog.NewTextHandler(io.Discard, nil))).writeServiceError(recorder, request, test.err)
		if recorder.Code != test.status {
			t.Fatalf("error %v status=%d want=%d", test.err, recorder.Code, test.status)
		}
	}
}
