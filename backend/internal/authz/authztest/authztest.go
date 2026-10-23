// Package authztest menyediakan fake authz untuk unit test modul lain (viewer,
// migration). Fake ini memakai Policy asli di atas repository in-memory,
// sehingga semantics-nya identik dengan production.
//
// Contoh:
//
//	store := authztest.NewStore()
//	viewer := store.AddUser("view-only-user", auth.PlatformRoleUser)
//	editor := store.AddUser("editor", auth.PlatformRoleUser)
//	store.AddDatabase(databaseID, true)
//	store.Grant("readers", databaseID, authz.AccessViewOnly, viewer.UserID)
//	store.Grant("writers", databaseID, authz.AccessViewEdit, editor.UserID)
//	policy := store.Policy()
package authztest

import (
	"context"
	"sync"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
)

// Store adalah repository authz in-memory yang aman dipakai paralel.
type Store struct {
	mu        sync.Mutex
	users     map[string]bool            // userID -> active
	databases map[string]bool            // databaseID -> active
	members   map[string]map[string]bool // group -> userID
	grants    map[string]map[string]authz.AccessLevel
	// Err, bila diisi, dikembalikan oleh seluruh query untuk mensimulasikan DB down.
	Err error
}

var _ authz.Repository = (*Store)(nil)

func NewStore() *Store {
	return &Store{
		users:     make(map[string]bool),
		databases: make(map[string]bool),
		members:   make(map[string]map[string]bool),
		grants:    make(map[string]map[string]authz.AccessLevel),
	}
}

// Policy mengembalikan authz.Policy asli yang membaca store ini.
func (s *Store) Policy() *authz.Policy {
	return authz.NewPolicy(s)
}

// AddUser mendaftarkan user aktif dan mengembalikan principal-nya.
func (s *Store) AddUser(username string, role auth.PlatformRole) auth.Principal {
	id, err := uuid.New()
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[id] = true
	return auth.Principal{UserID: id, Username: username, PlatformRole: role}
}

// SetUserActive mengubah status aktif user.
func (s *Store) SetUserActive(userID string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[userID] = active
}

// AddDatabase mendaftarkan managed database.
func (s *Store) AddDatabase(databaseID string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.databases[databaseID] = active
}

// AddMember memasukkan user ke group.
func (s *Store) AddMember(group, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.members[group] == nil {
		s.members[group] = make(map[string]bool)
	}
	s.members[group][userID] = true
}

// Grant memberi level ke group untuk database lalu (opsional) menambah anggota.
func (s *Store) Grant(group, databaseID string, level authz.AccessLevel, memberIDs ...string) {
	s.mu.Lock()
	if s.grants[group] == nil {
		s.grants[group] = make(map[string]authz.AccessLevel)
	}
	s.grants[group][databaseID] = level
	s.mu.Unlock()
	for _, memberID := range memberIDs {
		s.AddMember(group, memberID)
	}
}

func (s *Store) AccessFacts(_ context.Context, userID, databaseID string) (authz.AccessFacts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return authz.AccessFacts{}, s.Err
	}
	active, exists := s.users[userID]
	if !exists {
		return authz.AccessFacts{GrantedLevel: authz.AccessNone}, nil
	}
	databaseActive, databaseExists := s.databases[databaseID]
	return authz.AccessFacts{
		UserExists:     true,
		UserActive:     active,
		DatabaseExists: databaseExists,
		DatabaseActive: databaseActive,
		GrantedLevel:   s.levelLocked(userID, databaseID),
	}, nil
}

func (s *Store) ActiveDatabaseGrants(_ context.Context, userID string) ([]authz.DatabaseGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	if !s.users[userID] {
		return []authz.DatabaseGrant{}, nil
	}
	grants := make([]authz.DatabaseGrant, 0)
	for databaseID, active := range s.databases {
		if !active {
			continue
		}
		if level := s.levelLocked(userID, databaseID); level.CanView() {
			grants = append(grants, authz.DatabaseGrant{DatabaseID: databaseID, Level: level})
		}
	}
	return grants, nil
}

func (s *Store) levelLocked(userID, databaseID string) authz.AccessLevel {
	levels := make([]authz.AccessLevel, 0)
	for group, members := range s.members {
		if members[userID] {
			levels = append(levels, s.grants[group][databaseID])
		}
	}
	return authz.Highest(levels...)
}
