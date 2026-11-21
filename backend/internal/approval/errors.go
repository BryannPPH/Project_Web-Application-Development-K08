package approval

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrNotFound          = errors.New("approval: migration not found")
	ErrInvalidInput      = errors.New("approval: invalid input")
	ErrNotRequiredLeader = errors.New("approval: only leaders of groups with access to this database can decide")
	ErrStaleRevision     = errors.New("approval: migration changed since it was reviewed")
	ErrNotReviewable     = errors.New("approval: migration is not in a reviewable state for this action")

	ErrApprovalRequired = errors.New("approval: required approvals are not complete")
)

// IncompleteError: dikembalikan RequireApproved saat migration BELUM boleh jalan.
// Membawa alasan yang bisa ditampilkan ke user ("menunggu: Analis Data").
type IncompleteError struct {
	Status  Status   // pending | rejected | blocked
	Waiting []string // nama group yang belum menyetujui / belum punya leader
}

func (e *IncompleteError) Error() string {
	if len(e.Waiting) == 0 {
		return fmt.Sprintf("approval %s", e.Status)
	}
	return fmt.Sprintf("approval %s; waiting for: %s", e.Status, strings.Join(e.Waiting, ", "))
}

// Is membuat errors.Is(err, ErrApprovalRequired) bernilai true untuk
// IncompleteError, walaupun keduanya objek berbeda.
func (e *IncompleteError) Is(target error) bool { return target == ErrApprovalRequired }

// PublicError = bentuk error yang aman dikirim ke frontend.
type PublicError struct {
	Status  int
	Code    string
	Message string
}

// Describe menerjemahkan error approval ke HTTP. ok=false = bukan error approval.
// Dipakai handler approval DAN handler migration (saat Execute ditolak gerbang).
func Describe(err error) (PublicError, bool) {
	var incomplete *IncompleteError
	switch {
	case errors.As(err, &incomplete):
		message := "all required group leaders must approve this migration revision first"
		switch incomplete.Status {
		case StatusRejected:
			message = "a group leader rejected this migration revision"
		case StatusBlocked:
			message = "a group with access to this database has no leader; ask a platform admin to assign one"
		}
		return PublicError{http.StatusConflict, "APPROVAL_REQUIRED", message}, true
	case errors.Is(err, ErrNotFound):
		return PublicError{http.StatusNotFound, "NOT_FOUND", "migration not found"}, true
	case errors.Is(err, ErrInvalidInput):
		return PublicError{http.StatusBadRequest, "INVALID_INPUT", "action, revision, decision or note is invalid"}, true
	case errors.Is(err, ErrNotRequiredLeader):
		return PublicError{http.StatusForbidden, "APPROVAL_NOT_LEADER", "only leaders of groups with access to this database can decide"}, true
	case errors.Is(err, ErrStaleRevision):
		return PublicError{http.StatusConflict, "APPROVAL_STALE_REVISION", "migration was edited; reload and review the new revision"}, true
	case errors.Is(err, ErrNotReviewable):
		return PublicError{http.StatusConflict, "APPROVAL_NOT_REVIEWABLE", "migration cannot be reviewed for this action in its current state"}, true
	default:
		return PublicError{}, false
	}
}
