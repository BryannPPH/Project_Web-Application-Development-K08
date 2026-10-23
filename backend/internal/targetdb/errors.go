package targetdb

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Error typed untuk consumer provider.
var (
	// ErrDatabaseNotFound: managed database tidak terdaftar.
	ErrDatabaseNotFound = errors.New("targetdb: managed database not found")
	// ErrDatabaseInactive: managed database dinonaktifkan admin.
	ErrDatabaseInactive = errors.New("targetdb: managed database is inactive")
	// ErrCredentialUnavailable: credential tidak dapat didekripsi (key salah/rusak).
	ErrCredentialUnavailable = errors.New("targetdb: credential unavailable")
	// ErrTargetUnavailable: koneksi ke target gagal (jaringan, auth, timeout, dll.).
	ErrTargetUnavailable = errors.New("targetdb: target database unavailable")
)

// FailureReason adalah kategori kegagalan yang aman ditampilkan/di-log.
type FailureReason string

const (
	ReasonTimeout          FailureReason = "timeout"
	ReasonAuthentication   FailureReason = "authentication_failed"
	ReasonDatabaseNotFound FailureReason = "database_not_found"
	ReasonNetwork          FailureReason = "network_unreachable"
	ReasonTLS              FailureReason = "tls_failed"
	ReasonTooManyClients   FailureReason = "too_many_connections"
	ReasonInvalidConfig    FailureReason = "invalid_configuration"
	ReasonUnknown          FailureReason = "unknown"
)

// UnavailableError membungkus kegagalan koneksi tanpa membawa pesan asli,
// karena pesan pgconn dapat memuat host/user/database target.
type UnavailableError struct {
	DatabaseID string
	Reason     FailureReason
	// SQLState hanya berisi kode SQLSTATE (mis. 28P01), aman untuk log.
	SQLState string
}

func (e *UnavailableError) Error() string {
	return "target database unavailable: " + string(e.Reason)
}

func (e *UnavailableError) Unwrap() error {
	return ErrTargetUnavailable
}

// newUnavailableError mengklasifikasikan error koneksi lalu membuang detailnya.
func newUnavailableError(databaseID string, err error) *UnavailableError {
	result := &UnavailableError{DatabaseID: databaseID, Reason: ClassifyConnectError(err)}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		result.SQLState = pgError.Code
	}
	return result
}

// ClassifyConnectError mengubah error koneksi menjadi kategori yang aman.
func ClassifyConnectError(err error) FailureReason {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrInvalidConfig) {
		return ReasonInvalidConfig
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch {
		case pgError.Code == "28P01" || pgError.Code == "28000":
			return ReasonAuthentication
		case pgError.Code == "3D000":
			return ReasonDatabaseNotFound
		case pgError.Code == "53300":
			return ReasonTooManyClients
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
		return ReasonTimeout
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return ReasonTimeout
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "tls") || strings.Contains(lower, "ssl") || strings.Contains(lower, "certificate"):
		return ReasonTLS
	case strings.Contains(lower, "timeout"):
		return ReasonTimeout
	}
	var opError *net.OpError
	var dnsError *net.DNSError
	if errors.As(err, &opError) || errors.As(err, &dnsError) ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "dial") {
		return ReasonNetwork
	}
	return ReasonUnknown
}

// PublicError adalah representasi aman error target untuk HTTP.
type PublicError struct {
	Status  int
	Code    string
	Message string
}

// Describe memetakan error provider menjadi response publik. ok=false bila
// error bukan berasal dari provider.
func Describe(err error) (PublicError, bool) {
	switch {
	case errors.Is(err, ErrDatabaseNotFound):
		return PublicError{http.StatusNotFound, "DATABASE_NOT_FOUND", "database not found"}, true
	case errors.Is(err, ErrDatabaseInactive):
		return PublicError{http.StatusConflict, "DATABASE_INACTIVE", "database is inactive"}, true
	case errors.Is(err, ErrCredentialUnavailable):
		return PublicError{http.StatusBadGateway, "TARGET_UNAVAILABLE", "target database is unavailable"}, true
	case errors.Is(err, ErrTargetUnavailable):
		return PublicError{http.StatusBadGateway, "TARGET_UNAVAILABLE", "target database is unavailable"}, true
	default:
		return PublicError{}, false
	}
}
