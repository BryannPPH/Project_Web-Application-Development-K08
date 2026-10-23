package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AccessFacts adalah fakta mentah yang dibutuhkan policy untuk satu pasangan
// user-database. Keputusan akhir tetap dibuat oleh Policy, bukan repository.
type AccessFacts struct {
	UserExists     bool
	UserActive     bool
	DatabaseExists bool
	DatabaseActive bool
	// GrantedLevel adalah level tertinggi dari seluruh group user untuk database.
	GrantedLevel AccessLevel
}

// DatabaseGrant adalah effective access user terhadap satu database aktif.
type DatabaseGrant struct {
	DatabaseID string
	Level      AccessLevel
}

// Repository menyimpan seluruh query membership/grant di satu tempat.
type Repository interface {
	AccessFacts(ctx context.Context, userID, databaseID string) (AccessFacts, error)
	ActiveDatabaseGrants(ctx context.Context, userID string) ([]DatabaseGrant, error)
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGRepository membaca grant langsung dari platform database setiap request,
// sehingga pencabutan grant berlaku seketika tanpa cache invalidation.
type PGRepository struct {
	database queryer
}

func NewPGRepository(database queryer) *PGRepository {
	return &PGRepository{database: database}
}

// rankExpression mengubah access_level menjadi angka agar MAX() memilih level tertinggi.
const rankExpression = `CASE gda.access_level WHEN 'view_edit' THEN 2 WHEN 'view_only' THEN 1 ELSE 0 END`

func (r *PGRepository) AccessFacts(ctx context.Context, userID, databaseID string) (AccessFacts, error) {
	query := `
		SELECT
			u.is_active,
			md.id IS NOT NULL,
			COALESCE(md.is_active, FALSE),
			COALESCE((
				SELECT MAX(` + rankExpression + `)
				FROM group_members gm
				JOIN group_database_access gda ON gda.group_id = gm.group_id
				WHERE gm.user_id = u.id AND gda.database_id = $2::uuid
			), 0)
		FROM users u
		LEFT JOIN managed_databases md ON md.id = $2::uuid
		WHERE u.id = $1::uuid`

	facts := AccessFacts{UserExists: true}
	var rank int
	err := r.database.QueryRow(ctx, query, userID, databaseID).Scan(
		&facts.UserActive,
		&facts.DatabaseExists,
		&facts.DatabaseActive,
		&rank,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessFacts{GrantedLevel: AccessNone}, nil
	}
	if err != nil {
		return AccessFacts{}, fmt.Errorf("load database access facts: %w", err)
	}
	facts.GrantedLevel = levelFromRank(rank)
	return facts, nil
}

func (r *PGRepository) ActiveDatabaseGrants(ctx context.Context, userID string) ([]DatabaseGrant, error) {
	query := `
		SELECT gda.database_id::text, MAX(` + rankExpression + `)
		FROM users u
		JOIN group_members gm ON gm.user_id = u.id
		JOIN group_database_access gda ON gda.group_id = gm.group_id
		JOIN managed_databases md ON md.id = gda.database_id
		WHERE u.id = $1::uuid AND u.is_active AND md.is_active
		GROUP BY gda.database_id`

	rows, err := r.database.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list database grants: %w", err)
	}
	defer rows.Close()

	grants := make([]DatabaseGrant, 0)
	for rows.Next() {
		var grant DatabaseGrant
		var rank int
		if err := rows.Scan(&grant.DatabaseID, &rank); err != nil {
			return nil, fmt.Errorf("scan database grant: %w", err)
		}
		grant.Level = levelFromRank(rank)
		if grant.Level.CanView() {
			grants = append(grants, grant)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database grants: %w", err)
	}
	return grants, nil
}
