package approval

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository = kontrak penyimpanan approval.
type Repository interface {
	Target(ctx context.Context, databaseID, migrationID string) (Target, error)
	RequiredGroups(ctx context.Context, databaseID string) ([]RequiredGroup, error)
	LatestDecisions(ctx context.Context, migrationID string, action Action, revision int64) (map[string]DecisionRecord, error)
	InsertDecision(ctx context.Context, decision NewDecision) error
}

// PGRepository: implementasi Repository memakai pgxpool (platform DB).
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository = constructor.
func NewPGRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

// Target membaca status dan revisi migration milik database tersebut.
func (r *PGRepository) Target(ctx context.Context, databaseID, migrationID string) (Target, error) {
	var target Target
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, database_id::text, status, revision
		FROM migration_entries
		WHERE database_id = $1::uuid AND id = $2::uuid`, databaseID, migrationID).
		Scan(&target.MigrationID, &target.DatabaseID, &target.Status, &target.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("load approval target: %w", err)
	}
	return target, nil
}

// RequiredGroups: semua group ber-grant ke database beserta leader aktifnya.
func (r *PGRepository) RequiredGroups(ctx context.Context, databaseID string) ([]RequiredGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT g.id::text, g.name, gda.access_level, u.id::text, u.username
		FROM group_database_access gda
		JOIN groups g ON g.id = gda.group_id
		LEFT JOIN group_leaders gl ON gl.group_id = g.id
		LEFT JOIN users u ON u.id = gl.user_id AND u.is_active
		WHERE gda.database_id = $1::uuid
		ORDER BY lower(g.name)`, databaseID)
	if err != nil {
		return nil, fmt.Errorf("list required approval groups: %w", err)
	}
	defer rows.Close()

	groups := make([]RequiredGroup, 0)
	for rows.Next() {
		var group RequiredGroup
		var leaderID, leaderName *string
		if err := rows.Scan(&group.GroupID, &group.GroupName, &group.AccessLevel, &leaderID, &leaderName); err != nil {
			return nil, fmt.Errorf("scan required approval group: %w", err)
		}
		if leaderID != nil && leaderName != nil {
			group.Leader = &Person{UserID: *leaderID, Username: *leaderName}
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate required approval groups: %w", err)
	}
	return groups, nil
}

// supersedingActions: approval rollback/forward hanya berlaku untuk satu siklus.
// Execution dari aksi di bawah ini yang dimulai SETELAH sebuah keputusan membuat
// keputusan itu usang, sehingga rollback kedua (setelah forward) atau forward kedua
// (setelah rollback kedua) wajib di-approve ulang. Resume rollback/forward yang
// terhenti di tengah tidak menggeser baseline, jadi approval lamanya tetap berlaku.
// Apply tidak punya siklus: retry memang memakai approval apply yang sama.
var supersedingActions = map[Action][]string{
	ActionRollback: {"apply", "retry", "forward"},
	ActionForward:  {"rollback"},
	ActionApply:    {},
}

// LatestDecisions: keputusan terbaru per approver untuk satu revisi (DISTINCT ON),
// hanya yang diputuskan setelah execution terakhir yang menggantikan siklus approval.
func (r *PGRepository) LatestDecisions(ctx context.Context, migrationID string, action Action, revision int64) (map[string]DecisionRecord, error) {
	superseding := supersedingActions[action]
	if superseding == nil {
		superseding = []string{}
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (a.approver_id)
			a.approver_id::text, u.username, a.decision, a.note, a.decided_at
		FROM migration_approvals a
		JOIN users u ON u.id = a.approver_id
		WHERE a.migration_id = $1::uuid AND a.action = $2 AND a.revision = $3
		  AND a.decided_at > COALESCE((
		      SELECT max(e.requested_at) FROM migration_execution_attempts e
		      WHERE e.migration_id = $1::uuid AND e.action = ANY($4::text[])
		  ), '-infinity'::timestamptz)
		ORDER BY a.approver_id, a.decided_at DESC, a.id DESC`, migrationID, string(action), revision, superseding)
	if err != nil {
		return nil, fmt.Errorf("list approval decisions: %w", err)
	}
	defer rows.Close()

	decisions := make(map[string]DecisionRecord)
	for rows.Next() {
		var record DecisionRecord
		var decision string
		if err := rows.Scan(&record.ApproverID, &record.ApproverUsername, &decision, &record.Note, &record.DecidedAt); err != nil {
			return nil, fmt.Errorf("scan approval decision: %w", err)
		}
		record.Decision = Decision(decision)
		decisions[record.ApproverID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approval decisions: %w", err)
	}
	return decisions, nil
}

// InsertDecision menambah keputusan (append-only). WHERE revision = $3 mencegah
// keputusan tersimpan untuk revisi yang sudah diedit.
func (r *PGRepository) InsertDecision(ctx context.Context, decision NewDecision) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO migration_approvals (migration_id, action, revision, approver_id, decision, note, decided_at)
		SELECT m.id, $2, $3, $4::uuid, $5, $6, $7
		FROM migration_entries m
		WHERE m.id = $1::uuid AND m.revision = $3`,
		decision.MigrationID, string(decision.Action), decision.Revision, decision.ApproverID,
		string(decision.Decision), decision.Note, decision.DecidedAt)
	if err != nil {
		return fmt.Errorf("insert approval decision: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrStaleRevision
	}
	return nil
}
