// Package apihttp menyediakan helper HTTP kecil yang dipakai bersama oleh modul fitur.
//
// Bentuk response error identik dengan httpapi milik modul autentikasi:
//
//	{"error": {"code": "...", "message": "...", "requestId": "..."}}
//
// Request ID dibaca dari header X-Request-ID yang sudah dipasang middleware
// requestID sebelum handler fitur dipanggil, sehingga modul fitur tidak perlu
// mengakses context key milik httpapi.
package apihttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// MaxJSONBodyBytes membatasi ukuran body JSON request fitur.
const MaxJSONBodyBytes = 64 << 10

// ErrInvalidBody menandai body request yang tidak dapat di-decode.
var ErrInvalidBody = errors.New("invalid request body")

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	Field     string `json:"field,omitempty"`
}

// RequestID mengembalikan request ID yang sudah dipasang middleware.
func RequestID(w http.ResponseWriter) string {
	return w.Header().Get("X-Request-ID")
}

// WriteJSON menulis body JSON dengan status tertentu.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError menulis error publik dengan format yang konsisten.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorBody{Error: errorDetail{
		Code:      code,
		Message:   message,
		RequestID: RequestID(w),
	}})
}

// WriteFieldError menulis error validasi beserta nama field yang salah.
func WriteFieldError(w http.ResponseWriter, field, message string) {
	WriteJSON(w, http.StatusBadRequest, errorBody{Error: errorDetail{
		Code:      "VALIDATION_FAILED",
		Message:   message,
		RequestID: RequestID(w),
		Field:     field,
	}})
}

// InternalError mencatat detail internal lalu mengembalikan pesan generik.
func InternalError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, operation string, err error) {
	if logger != nil {
		logger.ErrorContext(r.Context(), operation,
			"request_id", RequestID(w),
			"error", err,
		)
	}
	WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}

// DecodeJSON membaca satu objek JSON dengan batas ukuran dan menolak field asing.
func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrInvalidBody
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidBody
	}
	return nil
}
