package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// DefaultTestTimeout membatasi test connection admin.
const DefaultTestTimeout = 5 * time.Second

// PingFunc membuka lalu menutup koneksi uji; diganti fake pada unit test.
type PingFunc func(ctx context.Context, config targetdb.ConnectionConfig, timeout time.Duration) error

// Service menangani administrasi managed database dan resolusi credential.
type Service struct {
	repository  Repository
	cipher      *CredentialCipher
	policy      authz.Service
	ping        PingFunc
	testTimeout time.Duration
	now         func() time.Time
	newID       func() (string, error)
}

var _ targetdb.ConfigResolver = (*Service)(nil)

func NewService(repository Repository, cipher *CredentialCipher, policy authz.Service, ping PingFunc) (*Service, error) {
	if repository == nil || cipher == nil || policy == nil {
		return nil, errors.New("managed database service requires repository, cipher, and policy")
	}
	if ping == nil {
		ping = targetdb.Ping
	}
	return &Service{
		repository:  repository,
		cipher:      cipher,
		policy:      policy,
		ping:        ping,
		testTimeout: DefaultTestTimeout,
		now:         time.Now,
		newID:       uuid.New,
	}, nil
}

// List mengembalikan seluruh managed database untuk admin.
func (s *Service) List(ctx context.Context, principal auth.Principal) ([]ManagedDatabase, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return nil, err
	}
	return s.repository.List(ctx)
}

// Get mengembalikan detail managed database untuk admin.
func (s *Service) Get(ctx context.Context, principal auth.Principal, id string) (ManagedDatabase, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return ManagedDatabase{}, err
	}
	if !uuid.Valid(id) {
		return ManagedDatabase{}, ErrNotFound
	}
	return s.repository.Get(ctx, id)
}

// Create memvalidasi input, mengenkripsi password dengan AAD = ID baru, lalu menyimpan.
func (s *Service) Create(ctx context.Context, principal auth.Principal, input CreateInput) (ManagedDatabase, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return ManagedDatabase{}, err
	}
	record, password, err := normalizeCreate(input)
	if err != nil {
		return ManagedDatabase{}, err
	}
	id, err := s.newID()
	if err != nil {
		return ManagedDatabase{}, err
	}
	record.ID = id
	credential, err := s.encrypt(id, password)
	if err != nil {
		return ManagedDatabase{}, err
	}
	return s.repository.Create(ctx, record, credential)
}

// Update menerapkan patch. Credential hanya diganti bila password dikirim eksplisit.
func (s *Service) Update(ctx context.Context, principal auth.Principal, id string, input UpdateInput) (ManagedDatabase, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return ManagedDatabase{}, err
	}
	if !uuid.Valid(id) {
		return ManagedDatabase{}, ErrNotFound
	}
	changes, password, err := normalizeUpdate(input)
	if err != nil {
		return ManagedDatabase{}, err
	}
	if password != nil {
		credential, err := s.encrypt(id, *password)
		if err != nil {
			return ManagedDatabase{}, err
		}
		changes.Credential = &credential
	}
	return s.repository.Update(ctx, id, changes)
}

// Delete menghapus koneksi. Database dengan riwayat migration ditolak (ErrInUse)
// oleh constraint ON DELETE RESTRICT; admin sebaiknya menonaktifkan saja.
func (s *Service) Delete(ctx context.Context, principal auth.Principal, id string) error {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return err
	}
	if !uuid.Valid(id) {
		return ErrNotFound
	}
	return s.repository.Delete(ctx, id)
}

// TestStored menguji koneksi memakai credential tersimpan.
func (s *Service) TestStored(ctx context.Context, principal auth.Principal, id string) (TestResult, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return TestResult{}, err
	}
	if !uuid.Valid(id) {
		return TestResult{}, ErrNotFound
	}
	config, err := s.resolve(ctx, id, false)
	if err != nil {
		if errors.Is(err, targetdb.ErrCredentialUnavailable) {
			return TestResult{Success: false, Reason: "credential_unavailable"}, nil
		}
		return TestResult{}, err
	}
	return s.test(ctx, config), nil
}

// TestDraft menguji parameter yang belum disimpan. Tidak ada yang ditulis ke
// database, baik test berhasil maupun gagal.
func (s *Service) TestDraft(ctx context.Context, principal auth.Principal, input CreateInput) (TestResult, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return TestResult{}, err
	}
	record, password, err := normalizeCreate(input)
	if err != nil {
		return TestResult{}, err
	}
	return s.test(ctx, targetdb.ConnectionConfig{
		DatabaseID: "draft",
		Host:       record.Host,
		Port:       record.Port,
		Database:   record.DatabaseName,
		Username:   record.Username,
		Password:   password,
		SSLMode:    record.SSLMode,
	}), nil
}

func (s *Service) test(ctx context.Context, config targetdb.ConnectionConfig) TestResult {
	startedAt := s.now()
	err := s.ping(ctx, config, s.testTimeout)
	config.Password = ""
	result := TestResult{Success: err == nil, LatencyMS: s.now().Sub(startedAt).Milliseconds()}
	if err != nil {
		var unavailable *targetdb.UnavailableError
		if errors.As(err, &unavailable) {
			result.Reason = string(unavailable.Reason)
		} else {
			result.Reason = string(targetdb.ClassifyConnectError(err))
		}
	}
	return result
}

// ListAccessible mengembalikan database yang di-grant ke user melalui group.
// Keputusan akses sepenuhnya berasal dari authz.
func (s *Service) ListAccessible(ctx context.Context, principal auth.Principal) ([]AccessibleDatabase, error) {
	grants, err := s.policy.AccessibleDatabases(ctx, principal)
	if err != nil {
		return nil, err
	}
	levels := make(map[string]authz.AccessLevel, len(grants))
	ids := make([]string, 0, len(grants))
	for _, grant := range grants {
		levels[grant.DatabaseID] = grant.Level
		ids = append(ids, grant.DatabaseID)
	}
	records, err := s.repository.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	accessible := make([]AccessibleDatabase, 0, len(records))
	for _, record := range records {
		if !record.IsActive {
			continue
		}
		accessible = append(accessible, toAccessible(record, levels[record.ID]))
	}
	return accessible, nil
}

// GetAccessible mengembalikan satu database bila user minimal view_only.
func (s *Service) GetAccessible(ctx context.Context, principal auth.Principal, id string) (AccessibleDatabase, error) {
	if err := s.policy.RequireDatabaseView(ctx, principal, id); err != nil {
		return AccessibleDatabase{}, err
	}
	level, err := s.policy.EffectiveDatabaseAccess(ctx, principal, id)
	if err != nil {
		return AccessibleDatabase{}, err
	}
	record, err := s.repository.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return AccessibleDatabase{}, authz.ErrDatabaseNotFound
	}
	if err != nil {
		return AccessibleDatabase{}, err
	}
	return toAccessible(record, level), nil
}

// ResolveConnection mengimplementasikan targetdb.ConfigResolver. Tidak ada
// authorization di sini: caller (viewer/migration) wajib memanggil authz lebih dulu.
func (s *Service) ResolveConnection(ctx context.Context, databaseID string) (targetdb.ConnectionConfig, error) {
	if !uuid.Valid(databaseID) {
		return targetdb.ConnectionConfig{}, targetdb.ErrDatabaseNotFound
	}
	return s.resolve(ctx, databaseID, true)
}

func (s *Service) resolve(ctx context.Context, id string, requireActive bool) (targetdb.ConnectionConfig, error) {
	record, credential, err := s.repository.GetCredential(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return targetdb.ConnectionConfig{}, targetdb.ErrDatabaseNotFound
	}
	if err != nil {
		return targetdb.ConnectionConfig{}, err
	}
	if requireActive && !record.IsActive {
		return targetdb.ConnectionConfig{}, targetdb.ErrDatabaseInactive
	}
	plaintext, err := s.cipher.Decrypt(record.ID, credential.Ciphertext, credential.KeyVersion)
	if err != nil {
		return targetdb.ConnectionConfig{}, fmt.Errorf("%w: %v", targetdb.ErrCredentialUnavailable, err)
	}
	config := targetdb.ConnectionConfig{
		DatabaseID: record.ID,
		Host:       record.Host,
		Port:       record.Port,
		Database:   record.DatabaseName,
		Username:   record.Username,
		Password:   string(plaintext),
		SSLMode:    record.SSLMode,
	}
	clear(plaintext)
	return config, nil
}

func (s *Service) encrypt(id, password string) (StoredCredential, error) {
	plaintext := []byte(password)
	defer clear(plaintext)
	ciphertext, version, err := s.cipher.Encrypt(id, plaintext)
	if err != nil {
		return StoredCredential{}, err
	}
	return StoredCredential{Ciphertext: ciphertext, KeyVersion: version}, nil
}

func toAccessible(record ManagedDatabase, level authz.AccessLevel) AccessibleDatabase {
	return AccessibleDatabase{
		ID:           record.ID,
		Name:         record.Name,
		DatabaseName: record.DatabaseName,
		AccessLevel:  level,
	}
}

func normalizeCreate(input CreateInput) (ManagedDatabase, string, error) {
	record := ManagedDatabase{
		Name:         strings.TrimSpace(input.Name),
		Host:         strings.TrimSpace(input.Host),
		Port:         input.Port,
		DatabaseName: strings.TrimSpace(input.DatabaseName),
		Username:     strings.TrimSpace(input.Username),
		SSLMode:      strings.TrimSpace(input.SSLMode),
		IsActive:     true,
	}
	if record.Port == 0 {
		record.Port = 5432
	}
	if record.SSLMode == "" {
		record.SSLMode = targetdb.DefaultSSLMode
	}
	if input.IsActive != nil {
		record.IsActive = *input.IsActive
	}
	if err := validateName(record.Name); err != nil {
		return ManagedDatabase{}, "", err
	}
	if err := validateConnectionFields(record.Host, record.Port, record.DatabaseName, record.Username, record.SSLMode); err != nil {
		return ManagedDatabase{}, "", err
	}
	if !targetdb.ValidPassword(input.Password) {
		return ManagedDatabase{}, "", &ValidationError{"password", "password is required and must be at most 1024 bytes"}
	}
	return record, input.Password, nil
}

func normalizeUpdate(input UpdateInput) (patch, *string, error) {
	var changes patch
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if err := validateName(name); err != nil {
			return patch{}, nil, err
		}
		changes.Name = &name
	}
	if input.Host != nil {
		host := strings.TrimSpace(*input.Host)
		if !targetdb.ValidHost(host) {
			return patch{}, nil, &ValidationError{"host", "host must be a hostname or IP address"}
		}
		changes.Host = &host
	}
	if input.Port != nil {
		if *input.Port < 1 || *input.Port > 65535 {
			return patch{}, nil, &ValidationError{"port", "port must be between 1 and 65535"}
		}
		changes.Port = input.Port
	}
	if input.DatabaseName != nil {
		value := strings.TrimSpace(*input.DatabaseName)
		if !targetdb.ValidIdentifierValue(value) {
			return patch{}, nil, &ValidationError{"databaseName", "database name must contain 1 to 63 bytes"}
		}
		changes.DatabaseName = &value
	}
	if input.Username != nil {
		value := strings.TrimSpace(*input.Username)
		if !targetdb.ValidIdentifierValue(value) {
			return patch{}, nil, &ValidationError{"username", "username must contain 1 to 63 bytes"}
		}
		changes.Username = &value
	}
	if input.SSLMode != nil {
		value := strings.TrimSpace(*input.SSLMode)
		if !targetdb.ValidSSLMode(value) {
			return patch{}, nil, &ValidationError{"sslMode", "ssl mode is not supported"}
		}
		changes.SSLMode = &value
	}
	changes.IsActive = input.IsActive
	if input.Password != nil && !targetdb.ValidPassword(*input.Password) {
		return patch{}, nil, &ValidationError{"password", "password must contain 1 to 1024 bytes"}
	}
	return changes, input.Password, nil
}

func validateName(name string) error {
	if length := utf8.RuneCountInString(name); length < 1 || length > 100 {
		return &ValidationError{"name", "name must contain between 1 and 100 characters"}
	}
	return nil
}

func validateConnectionFields(host string, port int, databaseName, username, sslMode string) error {
	switch {
	case !targetdb.ValidHost(host):
		return &ValidationError{"host", "host must be a hostname or IP address"}
	case port < 1 || port > 65535:
		return &ValidationError{"port", "port must be between 1 and 65535"}
	case !targetdb.ValidIdentifierValue(databaseName):
		return &ValidationError{"databaseName", "database name must contain 1 to 63 bytes"}
	case !targetdb.ValidIdentifierValue(username):
		return &ValidationError{"username", "username must contain 1 to 63 bytes"}
	case !targetdb.ValidSSLMode(sslMode):
		return &ValidationError{"sslMode", "ssl mode is not supported"}
	}
	return nil
}
