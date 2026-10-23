package group

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
)

// Repository menyimpan group, membership, dan grant.
type Repository interface {
	List(ctx context.Context) ([]Group, error)
	Get(ctx context.Context, id string) (Group, error)
	Create(ctx context.Context, name string, description *string) (Group, error)
	Update(ctx context.Context, id string, name *string, description *string, clearDescription bool) (Group, error)
	Delete(ctx context.Context, id string) error

	ListMembers(ctx context.Context, groupID string) ([]Member, error)
	AddMember(ctx context.Context, groupID, userID string, revoke RevokeUsers) (bool, error)
	RemoveMember(ctx context.Context, groupID, userID string) (bool, error)

	ListGrantsByGroup(ctx context.Context, groupID string) ([]Grant, error)
	ListGrantsByDatabase(ctx context.Context, databaseID string) ([]Grant, error)
	UpsertGrant(ctx context.Context, groupID, databaseID string, level authz.AccessLevel, revoke RevokeUsers) (Grant, error)
	DeleteGrant(ctx context.Context, groupID, databaseID string) error
}

type PGRepository struct {
	database *pgxpool.Pool
}

func NewPGRepository(database *pgxpool.Pool) *PGRepository {
	return &PGRepository{database: database}
}

const groupColumns = `
	g.id::text, g.name, g.description,
	(SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id)::int,
	g.created_at, g.updated_at`

func scanGroup(row pgx.Row) (Group, error) {
	var record Group
	err := row.Scan(&record.ID, &record.Name, &record.Description, &record.MemberCount, &record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func (r *PGRepository) List(ctx context.Context) ([]Group, error) {
	rows, err := r.database.Query(ctx, `SELECT `+groupColumns+` FROM groups g ORDER BY lower(g.name)`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()
	groups := make([]Group, 0)
	for rows.Next() {
		record, err := scanGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups: %w", err)
	}
	return groups, nil
}

func (r *PGRepository) Get(ctx context.Context, id string) (Group, error) {
	record, err := scanGroup(r.database.QueryRow(ctx, `SELECT `+groupColumns+` FROM groups g WHERE g.id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	if err != nil {
		return Group{}, fmt.Errorf("get group: %w", err)
	}
	return record, nil
}

func (r *PGRepository) Create(ctx context.Context, name string, description *string) (Group, error) {
	var id string
	err := r.database.QueryRow(ctx,
		`INSERT INTO groups (name, description) VALUES ($1, $2) RETURNING id::text`,
		name, description).Scan(&id)
	if err != nil {
		return Group{}, mapWriteError("create group", err)
	}
	return r.Get(ctx, id)
}

func (r *PGRepository) Update(ctx context.Context, id string, name *string, description *string, clearDescription bool) (Group, error) {
	tag, err := r.database.Exec(ctx, `
		UPDATE groups
		SET name = COALESCE($2, name),
			description = CASE WHEN $4 THEN NULL ELSE COALESCE($3, description) END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid`, id, name, description, clearDescription)
	if err != nil {
		return Group{}, mapWriteError("update group", err)
	}
	if tag.RowsAffected() == 0 {
		return Group{}, ErrNotFound
	}
	return r.Get(ctx, id)
}

// Delete menghapus group; membership dan grant ikut terhapus (ON DELETE CASCADE).
func (r *PGRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.database.Exec(ctx, `DELETE FROM groups WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) ListMembers(ctx context.Context, groupID string) ([]Member, error) {
	if _, err := r.Get(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := r.database.Query(ctx, `
		SELECT u.id::text, u.username, u.platform_role, u.is_active, gm.created_at
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = $1::uuid
		ORDER BY lower(u.username)`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()
	members := make([]Member, 0)
	for rows.Next() {
		var member Member
		var role string
		if err := rows.Scan(&member.UserID, &member.Username, &role, &member.IsActive, &member.AddedAt); err != nil {
			return nil, fmt.Errorf("scan group member: %w", err)
		}
		member.PlatformRole = auth.PlatformRole(role)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group members: %w", err)
	}
	return members, nil
}

// AddMember idempotent: bila sudah anggota, tidak ada perubahan dan added=false.
func (r *PGRepository) AddMember(ctx context.Context, groupID, userID string, revoke RevokeUsers) (bool, error) {
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin add member: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if err := requireRow(ctx, transaction, `SELECT 1 FROM groups WHERE id = $1::uuid FOR SHARE`, groupID, ErrNotFound); err != nil {
		return false, err
	}
	if err := requireRow(ctx, transaction, `SELECT 1 FROM users WHERE id = $1::uuid FOR SHARE`, userID, ErrUserNotFound); err != nil {
		return false, err
	}

	tag, err := transaction.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (group_id, user_id) DO NOTHING`, groupID, userID)
	if err != nil {
		return false, fmt.Errorf("add group member: %w", err)
	}
	added := tag.RowsAffected() == 1

	if added && revoke != nil {
		var hasGrant bool
		if err := transaction.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM group_database_access WHERE group_id = $1::uuid)`,
			groupID).Scan(&hasGrant); err != nil {
			return false, fmt.Errorf("check group grants: %w", err)
		}
		if hasGrant {
			if err := revoke([]string{userID}); err != nil {
				return false, err
			}
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit add member: %w", err)
	}
	return added, nil
}

// RemoveMember idempotent: removed=false bila user memang bukan anggota.
func (r *PGRepository) RemoveMember(ctx context.Context, groupID, userID string) (bool, error) {
	if _, err := r.Get(ctx, groupID); err != nil {
		return false, err
	}
	tag, err := r.database.Exec(ctx,
		`DELETE FROM group_members WHERE group_id = $1::uuid AND user_id = $2::uuid`, groupID, userID)
	if err != nil {
		return false, fmt.Errorf("remove group member: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

const grantSelect = `
	SELECT g.id::text, g.name, md.id::text, md.name, gda.access_level, gda.created_at, gda.updated_at
	FROM group_database_access gda
	JOIN groups g ON g.id = gda.group_id
	JOIN managed_databases md ON md.id = gda.database_id`

func scanGrants(rows pgx.Rows) ([]Grant, error) {
	defer rows.Close()
	grants := make([]Grant, 0)
	for rows.Next() {
		var grant Grant
		var level string
		if err := rows.Scan(&grant.GroupID, &grant.GroupName, &grant.DatabaseID, &grant.DatabaseName,
			&level, &grant.CreatedAt, &grant.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		grant.AccessLevel = authz.AccessLevel(level)
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grants: %w", err)
	}
	return grants, nil
}

func (r *PGRepository) ListGrantsByGroup(ctx context.Context, groupID string) ([]Grant, error) {
	if _, err := r.Get(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := r.database.Query(ctx, grantSelect+` WHERE gda.group_id = $1::uuid ORDER BY lower(md.name)`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group grants: %w", err)
	}
	return scanGrants(rows)
}

func (r *PGRepository) ListGrantsByDatabase(ctx context.Context, databaseID string) ([]Grant, error) {
	var exists bool
	if err := r.database.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM managed_databases WHERE id = $1::uuid)`, databaseID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check managed database: %w", err)
	}
	if !exists {
		return nil, ErrDatabaseNotFound
	}
	rows, err := r.database.Query(ctx, grantSelect+` WHERE gda.database_id = $1::uuid ORDER BY lower(g.name)`, databaseID)
	if err != nil {
		return nil, fmt.Errorf("list database grants: %w", err)
	}
	return scanGrants(rows)
}

// UpsertGrant membuat/mengubah grant. Bila level naik (none->any, view_only->view_edit),
// seluruh anggota group dicabut session-nya sebelum COMMIT.
func (r *PGRepository) UpsertGrant(
	ctx context.Context,
	groupID, databaseID string,
	level authz.AccessLevel,
	revoke RevokeUsers,
) (Grant, error) {
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return Grant{}, fmt.Errorf("begin upsert grant: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if err := requireRow(ctx, transaction, `SELECT 1 FROM groups WHERE id = $1::uuid FOR SHARE`, groupID, ErrNotFound); err != nil {
		return Grant{}, err
	}
	if err := requireRow(ctx, transaction, `SELECT 1 FROM managed_databases WHERE id = $1::uuid FOR SHARE`, databaseID, ErrDatabaseNotFound); err != nil {
		return Grant{}, err
	}

	previous := authz.AccessNone
	var previousRaw string
	err = transaction.QueryRow(ctx, `
		SELECT access_level FROM group_database_access
		WHERE group_id = $1::uuid AND database_id = $2::uuid
		FOR UPDATE`, groupID, databaseID).Scan(&previousRaw)
	switch {
	case err == nil:
		previous = authz.AccessLevel(previousRaw)
	case !errors.Is(err, pgx.ErrNoRows):
		return Grant{}, fmt.Errorf("load existing grant: %w", err)
	}

	if _, err := transaction.Exec(ctx, `
		INSERT INTO group_database_access (group_id, database_id, access_level)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (group_id, database_id)
		DO UPDATE SET access_level = EXCLUDED.access_level, updated_at = CURRENT_TIMESTAMP`,
		groupID, databaseID, string(level)); err != nil {
		return Grant{}, fmt.Errorf("upsert grant: %w", err)
	}

	elevated := !previous.AtLeast(level)
	if elevated && revoke != nil {
		memberIDs, err := memberIDs(ctx, transaction, groupID)
		if err != nil {
			return Grant{}, err
		}
		if len(memberIDs) > 0 {
			if err := revoke(memberIDs); err != nil {
				return Grant{}, err
			}
		}
	}

	rows, err := transaction.Query(ctx, grantSelect+`
		WHERE gda.group_id = $1::uuid AND gda.database_id = $2::uuid`, groupID, databaseID)
	if err != nil {
		return Grant{}, fmt.Errorf("load upserted grant: %w", err)
	}
	grants, err := scanGrants(rows)
	if err != nil {
		return Grant{}, err
	}
	if len(grants) != 1 {
		return Grant{}, errors.New("upserted grant was not found")
	}
	if err := transaction.Commit(ctx); err != nil {
		return Grant{}, fmt.Errorf("commit upsert grant: %w", err)
	}
	return grants[0], nil
}

// DeleteGrant mencabut grant. Karena authz membaca grant per request,
// pencabutan berlaku seketika tanpa perlu revoke session.
func (r *PGRepository) DeleteGrant(ctx context.Context, groupID, databaseID string) error {
	tag, err := r.database.Exec(ctx,
		`DELETE FROM group_database_access WHERE group_id = $1::uuid AND database_id = $2::uuid`,
		groupID, databaseID)
	if err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGrantNotFound
	}
	return nil
}

func memberIDs(ctx context.Context, transaction pgx.Tx, groupID string) ([]string, error) {
	rows, err := transaction.Query(ctx, `SELECT user_id::text FROM group_members WHERE group_id = $1::uuid`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group member ids: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan group member id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func requireRow(ctx context.Context, transaction pgx.Tx, query, id string, notFound error) error {
	var one int
	err := transaction.QueryRow(ctx, query, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}
	if err != nil {
		return fmt.Errorf("check row existence: %w", err)
	}
	return nil
}

func mapWriteError(operation string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return ErrDuplicateName
	}
	return fmt.Errorf("%s: %w", operation, err)
}
