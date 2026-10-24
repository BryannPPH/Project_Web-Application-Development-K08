package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type memoryRepository struct {
	records     map[string]ManagedDatabase
	credentials map[string]StoredCredential
	calls       int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		records:     make(map[string]ManagedDatabase),
		credentials: make(map[string]StoredCredential),
	}
}

func (r *memoryRepository) List(context.Context) ([]ManagedDatabase, error) {
	r.calls++
	records := make([]ManagedDatabase, 0, len(r.records))
	for _, record := range r.records {
		records = append(records, record)
	}
	return records, nil
}

func (r *memoryRepository) ListByIDs(_ context.Context, ids []string) ([]ManagedDatabase, error) {
	r.calls++
	records := make([]ManagedDatabase, 0)
	for _, id := range ids {
		if record, ok := r.records[id]; ok {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *memoryRepository) Get(_ context.Context, id string) (ManagedDatabase, error) {
	r.calls++
	record, ok := r.records[id]
	if !ok {
		return ManagedDatabase{}, ErrNotFound
	}
	return record, nil
}

func (r *memoryRepository) GetCredential(_ context.Context, id string) (ManagedDatabase, StoredCredential, error) {
	r.calls++
	record, ok := r.records[id]
	if !ok {
		return ManagedDatabase{}, StoredCredential{}, ErrNotFound
	}
	return record, r.credentials[id], nil
}

func (r *memoryRepository) Create(_ context.Context, record ManagedDatabase, credential StoredCredential) (ManagedDatabase, error) {
	r.calls++
	record.HasCredential = true
	r.records[record.ID] = record
	r.credentials[record.ID] = credential
	return record, nil
}

func (r *memoryRepository) Update(_ context.Context, id string, changes patch) (ManagedDatabase, error) {
	r.calls++
	record, ok := r.records[id]
	if !ok {
		return ManagedDatabase{}, ErrNotFound
	}
	if changes.Name != nil {
		record.Name = *changes.Name
	}
	if changes.Engine != nil {
		record.Engine = *changes.Engine
	}
	if changes.Host != nil {
		record.Host = *changes.Host
	}
	if changes.Port != nil {
		record.Port = *changes.Port
	}
	if changes.DatabaseName != nil {
		record.DatabaseName = *changes.DatabaseName
	}
	if changes.Username != nil {
		record.Username = *changes.Username
	}
	if changes.SSLMode != nil {
		record.SSLMode = *changes.SSLMode
	}
	if changes.IsActive != nil {
		record.IsActive = *changes.IsActive
	}
	if changes.Credential != nil {
		r.credentials[id] = *changes.Credential
	}
	r.records[id] = record
	return record, nil
}

func (r *memoryRepository) Delete(_ context.Context, id string) error {
	r.calls++
	delete(r.records, id)
	return nil
}

type fixture struct {
	service    *Service
	repository *memoryRepository
	store      *authztest.Store
	admin      auth.Principal
	user       auth.Principal
	pings      []targetdb.ConnectionConfig
	pingErr    error
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cipher, err := NewCredentialCipher(testKey(), 1)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{repository: newMemoryRepository(), store: authztest.NewStore()}
	f.admin = f.store.AddUser("admin", auth.PlatformRoleAdmin)
	f.user = f.store.AddUser("user", auth.PlatformRoleUser)
	f.service, err = NewService(f.repository, cipher, f.store.Policy(), func(_ context.Context, config targetdb.ConnectionConfig, _ time.Duration) error {
		f.pings = append(f.pings, config)
		return f.pingErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func validInput() CreateInput {
	return CreateInput{
		Engine:       targetdb.EnginePostgreSQL,
		Name:         "Target",
		Host:         "localhost",
		Port:         55433,
		DatabaseName: "target_db",
		Username:     "target_owner",
		Password:     "target_dev_password",
		SSLMode:      "disable",
	}
}

func TestCreateMySQLDefaultsAndCapabilities(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	input := validInput()
	input.Engine = targetdb.EngineMySQL
	input.Port = 0
	input.SSLMode = ""
	created, err := f.service.Create(context.Background(), f.admin, input)
	if err != nil {
		t.Fatalf("Create(MySQL) error = %v", err)
	}
	if created.Port != 3306 || created.SSLMode != "require" || created.Engine != targetdb.EngineMySQL {
		t.Fatalf("Create(MySQL) = %+v", created)
	}
	f.store.AddDatabase(created.ID, true)
	f.store.Grant("mysql-readers", created.ID, authz.AccessViewOnly, f.user.UserID)
	detail, err := f.service.GetAccessible(context.Background(), f.user, created.ID)
	if err != nil {
		t.Fatalf("GetAccessible(MySQL) error = %v", err)
	}
	if !detail.Capabilities.Viewer || !detail.Capabilities.TransactionalMigration || detail.Capabilities.TransactionalDDL {
		t.Fatalf("MySQL capabilities = %+v", detail.Capabilities)
	}
}

func TestUpdateRejectsEngineChange(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	created, err := f.service.Create(context.Background(), f.admin, validInput())
	if err != nil {
		t.Fatal(err)
	}
	engine := targetdb.EngineMySQL
	_, err = f.service.Update(context.Background(), f.admin, created.ID, UpdateInput{Engine: &engine})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "engine" {
		t.Fatalf("Update(engine) error = %v", err)
	}
}

func TestNonAdminCannotManageDatabases(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service.List(ctx, f.user); !errors.Is(err, authz.ErrPlatformAdminRequired) {
		t.Errorf("List() error = %v", err)
	}
	if _, err := f.service.Create(ctx, f.user, validInput()); !errors.Is(err, authz.ErrPlatformAdminRequired) {
		t.Errorf("Create() error = %v", err)
	}
	if _, err := f.service.TestDraft(ctx, f.user, validInput()); !errors.Is(err, authz.ErrPlatformAdminRequired) {
		t.Errorf("TestDraft() error = %v", err)
	}
	if f.repository.calls != 0 || len(f.pings) != 0 {
		t.Fatal("policy must be checked before touching repository or target")
	}
}

func TestCreateEncryptsCredentialBoundToID(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	created, err := f.service.Create(context.Background(), f.admin, validInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.HasCredential || created.SSLMode != "disable" || !created.IsActive {
		t.Fatalf("Create() = %+v", created)
	}
	stored := f.repository.credentials[created.ID]
	plaintext, err := f.service.cipher.Decrypt(created.ID, stored.Ciphertext, stored.KeyVersion)
	if err != nil || string(plaintext) != "target_dev_password" {
		t.Fatalf("stored credential decrypt = %q, %v", plaintext, err)
	}

	config, err := f.service.ResolveConnection(context.Background(), created.ID)
	if err != nil || config.Password != "target_dev_password" || config.Port != 55433 {
		t.Fatalf("ResolveConnection() = %v, %v", config, err)
	}
}

func TestCreateValidation(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cases := map[string]func(*CreateInput){
		"name":         func(i *CreateInput) { i.Name = " " },
		"host":         func(i *CreateInput) { i.Host = "host name" },
		"port":         func(i *CreateInput) { i.Port = 70000 },
		"databaseName": func(i *CreateInput) { i.DatabaseName = "" },
		"username":     func(i *CreateInput) { i.Username = "" },
		"password":     func(i *CreateInput) { i.Password = "" },
		"sslMode":      func(i *CreateInput) { i.SSLMode = "sometimes" },
	}
	for field, mutate := range cases {
		input := validInput()
		mutate(&input)
		_, err := f.service.Create(context.Background(), f.admin, input)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != field {
			t.Errorf("Create(%s) error = %v, want validation on %s", field, err, field)
		}
	}
	if f.repository.calls != 0 {
		t.Fatal("invalid input reached repository")
	}

	defaults := validInput()
	defaults.Port = 0
	defaults.SSLMode = ""
	created, err := f.service.Create(context.Background(), f.admin, defaults)
	if err != nil || created.Port != 5432 || created.SSLMode != "require" {
		t.Fatalf("Create(defaults) = %+v, %v; want port 5432 sslmode require", created, err)
	}
}

func TestUpdateKeepsCredentialUnlessPasswordProvided(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	created, _ := f.service.Create(ctx, f.admin, validInput())
	original := f.repository.credentials[created.ID]

	name := "Renamed"
	if _, err := f.service.Update(ctx, f.admin, created.ID, UpdateInput{Name: &name}); err != nil {
		t.Fatalf("Update(name) error = %v", err)
	}
	if string(f.repository.credentials[created.ID].Ciphertext) != string(original.Ciphertext) {
		t.Fatal("credential changed without explicit password update")
	}

	password := "rotated-password"
	if _, err := f.service.Update(ctx, f.admin, created.ID, UpdateInput{Password: &password}); err != nil {
		t.Fatalf("Update(password) error = %v", err)
	}
	config, _ := f.service.ResolveConnection(ctx, created.ID)
	if config.Password != password {
		t.Fatal("password update was not stored")
	}
}

func TestTestDraftDoesNotPersist(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.pingErr = &targetdb.UnavailableError{Reason: targetdb.ReasonAuthentication}
	result, err := f.service.TestDraft(context.Background(), f.admin, validInput())
	if err != nil {
		t.Fatalf("TestDraft() error = %v", err)
	}
	if result.Success || result.Reason != string(targetdb.ReasonAuthentication) {
		t.Fatalf("TestDraft() = %+v", result)
	}
	if len(f.repository.records) != 0 || f.repository.calls != 0 {
		t.Fatal("TestDraft must not persist anything")
	}
}

func TestTestStoredUsesDecryptedCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	created, _ := f.service.Create(context.Background(), f.admin, validInput())
	result, err := f.service.TestStored(context.Background(), f.admin, created.ID)
	if err != nil || !result.Success {
		t.Fatalf("TestStored() = %+v, %v", result, err)
	}
	if len(f.pings) != 1 || f.pings[0].Password != "target_dev_password" {
		t.Fatal("TestStored did not use stored credential")
	}
	if _, err := f.service.TestStored(context.Background(), f.admin, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TestStored(missing) error = %v", err)
	}
}

func TestResolveConnectionRejectsInactiveAndUnknown(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	input := validInput()
	inactive := false
	input.IsActive = &inactive
	created, _ := f.service.Create(ctx, f.admin, input)

	if _, err := f.service.ResolveConnection(ctx, created.ID); !errors.Is(err, targetdb.ErrDatabaseInactive) {
		t.Errorf("ResolveConnection(inactive) error = %v", err)
	}
	if _, err := f.service.ResolveConnection(ctx, "11111111-1111-4111-8111-111111111111"); !errors.Is(err, targetdb.ErrDatabaseNotFound) {
		t.Errorf("ResolveConnection(unknown) error = %v", err)
	}
	if _, err := f.service.ResolveConnection(ctx, "x'; DROP TABLE users; --"); !errors.Is(err, targetdb.ErrDatabaseNotFound) {
		t.Errorf("ResolveConnection(invalid id) error = %v", err)
	}

	corrupted := f.repository.credentials[created.ID]
	corrupted.Ciphertext = append([]byte(nil), corrupted.Ciphertext...)
	corrupted.Ciphertext[0] ^= 0xff
	f.repository.credentials[created.ID] = corrupted
	active := true
	_, _ = f.service.Update(ctx, f.admin, created.ID, UpdateInput{IsActive: &active})
	if _, err := f.service.ResolveConnection(ctx, created.ID); !errors.Is(err, targetdb.ErrCredentialUnavailable) {
		t.Errorf("ResolveConnection(corrupted) error = %v", err)
	}
}

func TestListAccessibleUsesGroupGrantsOnly(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	granted, _ := f.service.Create(ctx, f.admin, validInput())
	other := validInput()
	other.Name = "Other"
	hidden, _ := f.service.Create(ctx, f.admin, other)
	f.store.AddDatabase(granted.ID, true)
	f.store.AddDatabase(hidden.ID, true)
	f.store.Grant("readers", granted.ID, authz.AccessViewOnly, f.user.UserID)

	databases, err := f.service.ListAccessible(ctx, f.user)
	if err != nil {
		t.Fatalf("ListAccessible() error = %v", err)
	}
	if len(databases) != 1 || databases[0].ID != granted.ID || databases[0].AccessLevel != authz.AccessViewOnly {
		t.Fatalf("ListAccessible() = %+v", databases)
	}

	adminDatabases, err := f.service.ListAccessible(ctx, f.admin)
	if err != nil || len(adminDatabases) != 0 {
		t.Fatalf("admin without grant ListAccessible() = %+v, %v; want empty", adminDatabases, err)
	}
	if _, err := f.service.GetAccessible(ctx, f.admin, granted.ID); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Fatalf("admin without grant GetAccessible() error = %v", err)
	}
	detail, err := f.service.GetAccessible(ctx, f.user, granted.ID)
	if err != nil || detail.Name != "Target" {
		t.Fatalf("GetAccessible() = %+v, %v", detail, err)
	}
}
