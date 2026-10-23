// Package database mengelola metadata koneksi managed database beserta
// credential terenkripsi, dan menyediakan resolver credential untuk targetdb.
package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
)

var (
	ErrNotFound      = errors.New("managed database not found")
	ErrDuplicateName = errors.New("managed database name already exists")
	// ErrInUse: database memiliki riwayat migration (FK ON DELETE RESTRICT).
	ErrInUse = errors.New("managed database has migration history")
)

// ValidationError menandai input admin yang tidak valid pada field tertentu.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ManagedDatabase adalah metadata koneksi tanpa credential.
type ManagedDatabase struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	DatabaseName  string    `json:"databaseName"`
	Username      string    `json:"username"`
	SSLMode       string    `json:"sslMode"`
	IsActive      bool      `json:"isActive"`
	HasCredential bool      `json:"hasCredential"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// StoredCredential adalah envelope terenkripsi. Tidak pernah di-encode ke JSON.
type StoredCredential struct {
	Ciphertext []byte
	KeyVersion int16
}

// AccessibleDatabase adalah DTO untuk user biasa: tanpa host/username/credential.
type AccessibleDatabase struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	DatabaseName string            `json:"databaseName"`
	AccessLevel  authz.AccessLevel `json:"accessLevel"`
}

// CreateInput adalah input pembuatan managed database.
type CreateInput struct {
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"databaseName"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSLMode      string `json:"sslMode"`
	IsActive     *bool  `json:"isActive"`
}

// UpdateInput adalah patch parsial. Password hanya diubah bila dikirim eksplisit.
type UpdateInput struct {
	Name         *string `json:"name"`
	Host         *string `json:"host"`
	Port         *int    `json:"port"`
	DatabaseName *string `json:"databaseName"`
	Username     *string `json:"username"`
	Password     *string `json:"password"`
	SSLMode      *string `json:"sslMode"`
	IsActive     *bool   `json:"isActive"`
}

// TestResult adalah hasil test connection yang aman ditampilkan.
type TestResult struct {
	Success   bool   `json:"success"`
	Reason    string `json:"reason,omitempty"`
	LatencyMS int64  `json:"latencyMs"`
}

// patch adalah UpdateInput yang sudah dinormalisasi untuk repository.
type patch struct {
	Name         *string
	Host         *string
	Port         *int
	DatabaseName *string
	Username     *string
	SSLMode      *string
	IsActive     *bool
	Credential   *StoredCredential
}
