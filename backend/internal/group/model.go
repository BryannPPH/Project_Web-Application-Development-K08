// Package group mengelola group, membership, dan grant akses database per group.
// Akses database tidak pernah diberikan langsung ke user; selalu lewat group.
package group

import (
	"errors"
	"fmt"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
)

var (
	ErrNotFound         = errors.New("group not found")
	ErrDuplicateName    = errors.New("group name already exists")
	ErrUserNotFound     = errors.New("user not found")
	ErrDatabaseNotFound = errors.New("managed database not found")
	ErrGrantNotFound    = errors.New("grant not found")
)

// ValidationError menandai input yang tidak valid pada field tertentu.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Member struct {
	UserID       string            `json:"userId"`
	Username     string            `json:"username"`
	PlatformRole auth.PlatformRole `json:"platformRole"`
	IsActive     bool              `json:"isActive"`
	AddedAt      time.Time         `json:"addedAt"`
}

type Grant struct {
	GroupID      string            `json:"groupId"`
	GroupName    string            `json:"groupName"`
	DatabaseID   string            `json:"databaseId"`
	DatabaseName string            `json:"databaseName"`
	AccessLevel  authz.AccessLevel `json:"accessLevel"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type CreateInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// UpdateInput: field nil tidak diubah; description "" menghapus deskripsi.
type UpdateInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type GrantInput struct {
	AccessLevel string `json:"accessLevel"`
}

// RevokeUsers dipanggil di dalam transaksi sebelum COMMIT dengan daftar user
// yang privilege-nya naik. Error membatalkan perubahan.
type RevokeUsers func(userIDs []string) error
