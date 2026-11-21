package approval

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
)

const maxNoteRunes = 1000

// Authorizer: bagian dari authz.Service yang kita butuhkan. Ditulis sebagai
// interface kecil supaya test bisa memakai authztest (policy palsu di memori).
type Authorizer interface {
	RequireDatabaseView(ctx context.Context, principal auth.Principal, databaseID string) error
}

// Clock: sumber waktu yang bisa dipalsukan di test.
type Clock interface{ Now() time.Time }

// RealClock: waktu sungguhan (UTC).
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// Service = aturan approval.
type Service struct {
	repository Repository
	authorizer Authorizer
	clock      Clock
}

// NewService = constructor. clock boleh nil (pakai RealClock).
func NewService(repository Repository, authorizer Authorizer, clock Clock) (*Service, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("approval service requires repository and authorizer")
	}
	if clock == nil {
		clock = RealClock{}
	}
	return &Service{repository: repository, authorizer: authorizer, clock: clock}, nil
}

// Review mengembalikan status review revisi sekarang (cukup akses view).
func (s *Service) Review(ctx context.Context, principal auth.Principal, databaseID, migrationID string, action Action) (Review, error) {
	if !uuid.Valid(databaseID) || !uuid.Valid(migrationID) || !action.Valid() {
		return Review{}, ErrInvalidInput
	}
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return Review{}, err
	}
	target, err := s.repository.Target(ctx, databaseID, migrationID)
	if err != nil {
		return Review{}, err
	}
	groups, decisions, err := s.load(ctx, target, action)
	if err != nil {
		return Review{}, err
	}
	status, reviews, approved := evaluate(groups, decisions)

	review := Review{
		MigrationID:   target.MigrationID,
		DatabaseID:    target.DatabaseID,
		Action:        action,
		Revision:      target.Revision,
		Status:        status,
		Groups:        reviews,
		ApprovedCount: approved,
		RequiredCount: len(groups),
		CanDecide:     isRequiredLeader(groups, principal.UserID) && reviewable(target.Status, action),
	}
	if mine, ok := decisions[principal.UserID]; ok {
		decision := mine.Decision
		review.MyDecision = &decision
	}
	return review, nil
}

// Decide menyimpan keputusan leader untuk revisi yang ia lihat. Hanya leader group
// wajib yang boleh; revisi basi ditolak (ErrStaleRevision).
func (s *Service) Decide(ctx context.Context, principal auth.Principal, databaseID, migrationID string, input DecideInput) (Review, error) {
	if !uuid.Valid(databaseID) || !uuid.Valid(migrationID) {
		return Review{}, ErrInvalidInput
	}
	note, err := normalizeInput(&input)
	if err != nil {
		return Review{}, err
	}
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return Review{}, err
	}
	target, err := s.repository.Target(ctx, databaseID, migrationID)
	if err != nil {
		return Review{}, err
	}
	if !reviewable(target.Status, input.Action) {
		return Review{}, ErrNotReviewable
	}
	if input.Revision != target.Revision {
		return Review{}, ErrStaleRevision
	}
	groups, err := s.repository.RequiredGroups(ctx, databaseID)
	if err != nil {
		return Review{}, err
	}
	if !isRequiredLeader(groups, principal.UserID) {
		return Review{}, ErrNotRequiredLeader
	}
	if err := s.repository.InsertDecision(ctx, NewDecision{
		MigrationID: migrationID,
		Action:      input.Action,
		Revision:    input.Revision,
		ApproverID:  principal.UserID,
		Decision:    input.Decision,
		Note:        note,
		DecidedAt:   s.clock.Now(),
	}); err != nil {
		return Review{}, err
	}
	return s.Review(ctx, principal, databaseID, migrationID, input.Action)
}

// RequireApproved adalah gerbang eksekusi: nil bila semua leader wajib sudah
// menyetujui revision ini, selain itu *IncompleteError.
func (s *Service) RequireApproved(ctx context.Context, databaseID, migrationID, action string, revision int64) error {
	act := Action(action)
	if !uuid.Valid(databaseID) || !uuid.Valid(migrationID) || !act.Valid() || revision < 1 {
		return ErrInvalidInput
	}
	groups, err := s.repository.RequiredGroups(ctx, databaseID)
	if err != nil {
		return err
	}
	decisions, err := s.repository.LatestDecisions(ctx, migrationID, act, revision)
	if err != nil {
		return err
	}
	status, reviews, _ := evaluate(groups, decisions)
	if status == StatusApproved {
		return nil
	}
	waiting := make([]string, 0)
	for _, review := range reviews {
		if review.Leader == nil || review.Decision == nil || *review.Decision != DecisionApproved {
			waiting = append(waiting, review.GroupName)
		}
	}
	return &IncompleteError{Status: status, Waiting: waiting}
}

// load: group wajib + keputusan terbaru untuk revisi target.
func (s *Service) load(ctx context.Context, target Target, action Action) ([]RequiredGroup, map[string]DecisionRecord, error) {
	groups, err := s.repository.RequiredGroups(ctx, target.DatabaseID)
	if err != nil {
		return nil, nil, err
	}
	decisions, err := s.repository.LatestDecisions(ctx, target.MigrationID, action, target.Revision)
	if err != nil {
		return nil, nil, err
	}
	return groups, decisions, nil
}

// reviewable: apply untuk draft, rollback untuk succeeded, forward untuk yang gagal/di-rollback.
func reviewable(migrationStatus string, action Action) bool {
	switch action {
	case ActionApply:
		return migrationStatus == "draft"
	case ActionRollback:
		return migrationStatus == "succeeded"
	case ActionForward:
		return migrationStatus == "failed" || migrationStatus == "reconciled" || migrationStatus == "rolled_back"
	default:
		return false
	}
}

// normalizeInput: validasi body POST. Mengembalikan note yang sudah di-trim
// (nil kalau kosong).
func normalizeInput(input *DecideInput) (*string, error) {
	if !input.Action.Valid() || !input.Decision.Valid() || input.Revision < 1 {
		return nil, ErrInvalidInput
	}
	var note *string
	if input.Note != nil {
		trimmed := strings.TrimSpace(*input.Note)
		if utf8.RuneCountInString(trimmed) > maxNoteRunes {
			return nil, ErrInvalidInput
		}
		if trimmed != "" {
			note = &trimmed
		}
	}
	if input.Decision == DecisionRejected && note == nil {
		return nil, ErrInvalidInput
	}
	return note, nil
}
