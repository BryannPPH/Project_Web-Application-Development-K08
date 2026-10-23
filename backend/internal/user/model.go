// Package user menyediakan administrasi user platform oleh platform admin.
//
// Penghapusan dilakukan sebagai deaktivasi (soft delete) agar membership dan
// riwayat migration tetap utuh. password_hash tidak pernah masuk DTO.
package user

import (
	"errors"
	"fmt"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

var (
	ErrNotFound          = errors.New("user not found")
	ErrDuplicateUsername = errors.New("username already exists")
	// ErrLastActiveAdmin mencegah platform kehilangan seluruh admin aktif.
	ErrLastActiveAdmin = errors.New("cannot demote or deactivate the last active platform admin")
)

// ValidationError menandai input yang tidak valid pada field tertentu.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// User adalah DTO allowlist; tidak ada password_hash.
type User struct {
	ID           string            `json:"id"`
	Username     string            `json:"username"`
	PlatformRole auth.PlatformRole `json:"platformRole"`
	IsActive     bool              `json:"isActive"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

// CreateInput adalah body POST /admin/users.
type CreateInput struct {
	Username     string            `json:"username"`
	Password     string            `json:"password"`
	PlatformRole auth.PlatformRole `json:"platformRole"`
	IsActive     *bool             `json:"isActive"`
}

// UpdateInput adalah body PATCH /admin/users/{id}; field nil tidak diubah.
type UpdateInput struct {
	Username     *string            `json:"username"`
	PlatformRole *auth.PlatformRole `json:"platformRole"`
	IsActive     *bool              `json:"isActive"`
}

// ResetPasswordInput adalah body POST /admin/users/{id}/reset-password.
type ResetPasswordInput struct {
	Password string `json:"password"`
}

// createRecord dan updateRecord adalah data yang sudah tervalidasi untuk repository.
type createRecord struct {
	Username     string
	PasswordHash string
	PlatformRole auth.PlatformRole
	IsActive     bool
}

type updateRecord struct {
	Username     *string
	PlatformRole *auth.PlatformRole
	IsActive     *bool
	PasswordHash *string
}

// BeforeCommit dijalankan di dalam transaksi setelah UPDATE dan sebelum COMMIT.
// Error dari hook membatalkan seluruh perubahan.
type BeforeCommit func(before, after User) error
