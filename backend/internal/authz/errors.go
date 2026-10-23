package authz

import (
	"errors"
	"net/http"
)

// Error policy bersifat typed agar setiap handler memetakan status secara sama.
var (
	// ErrUnauthenticated: principal kosong atau user sudah tidak aktif.
	ErrUnauthenticated = errors.New("authz: authentication required")
	// ErrPlatformAdminRequired: aksi administrasi oleh user non-admin.
	ErrPlatformAdminRequired = errors.New("authz: platform admin role required")
	// ErrDatabaseNotFound dipakai untuk database yang tidak ada MAUPUN yang tidak
	// di-grant, sehingga user tanpa akses tidak dapat menebak keberadaannya.
	ErrDatabaseNotFound = errors.New("authz: database not found")
	// ErrDatabaseInactive hanya muncul bila user memiliki grant, jadi tidak bocor.
	ErrDatabaseInactive = errors.New("authz: database is inactive")
	// ErrDatabaseEditForbidden: user memiliki view_only tetapi aksi butuh view_edit.
	ErrDatabaseEditForbidden = errors.New("authz: view_edit access required")
)

// PublicError adalah representasi aman sebuah error policy untuk HTTP.
type PublicError struct {
	Status  int
	Code    string
	Message string
}

// Describe memetakan error policy menjadi status/code/message publik.
// Nilai ok=false berarti error bukan berasal dari policy authz.
func Describe(err error) (PublicError, bool) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return PublicError{http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required"}, true
	case errors.Is(err, ErrPlatformAdminRequired):
		return PublicError{http.StatusForbidden, "FORBIDDEN", "platform admin role required"}, true
	case errors.Is(err, ErrDatabaseNotFound):
		return PublicError{http.StatusNotFound, "DATABASE_NOT_FOUND", "database not found"}, true
	case errors.Is(err, ErrDatabaseInactive):
		return PublicError{http.StatusConflict, "DATABASE_INACTIVE", "database is inactive"}, true
	case errors.Is(err, ErrDatabaseEditForbidden):
		return PublicError{http.StatusForbidden, "DATABASE_EDIT_FORBIDDEN", "view and edit access is required"}, true
	default:
		return PublicError{}, false
	}
}
