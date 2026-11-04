// base/workspace.go：对应 excel_workspace 表的数据操作。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrWorkspaceNotFound identifies a missing Base.
var ErrWorkspaceNotFound = errors.New("workspace Base not found")

// WorkspaceBase is the home/list projection of one Base.
type WorkspaceBase struct {
	ID             string
	EnterpriseID   string
	OwnerID        string
	Name           string
	TableCount     int
	MyRole         string
	Starred        bool
	LastModifiedAt int64
	LastModifier   string
	CreatedAt      int64
}

// WorkspaceRepository owns SQL access to excel_workspace_* tables.
type WorkspaceRepository struct {
	db *sql.DB
}

// NewWorkspaceRepository creates the workspace persistence gateway.
func NewWorkspaceRepository(db *sql.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

// BaseIDsByEnterprise lists every term Base id for tenant-scoped cleanup.
func (repository *WorkspaceRepository) BaseIDsByEnterprise(
	ctx context.Context,
	enterpriseID string,
) ([]string, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("workspace database is required")
	}
	rows, err := repository.db.QueryContext(ctx, `
		SELECT id
		FROM excel_workspace_bases
		WHERE enterprise_id = ?
		ORDER BY id
	`, enterpriseID)
	if err != nil {
		return nil, fmt.Errorf("list bases by enterprise: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan base id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate base ids: %w", err)
	}
	return ids, nil
}

// BeginTx starts one workspace mutation transaction.
func (repository *WorkspaceRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("workspace database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin workspace transaction: %w", err)
	}
	return tx, nil
}

// CreateBase inserts a Base and records it as the owner's most recent access.
func (repository *WorkspaceRepository) CreateBase(
	ctx context.Context,
	item WorkspaceBase,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("workspace database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workspace Base creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_workspace_bases (
			id, enterprise_id, owner_id, name, last_modified_at,
			last_modifier, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.EnterpriseID, item.OwnerID, item.Name,
		item.LastModifiedAt, item.LastModifier, item.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert workspace Base: %w", err)
	}
	if err := upsertRecent(ctx, tx, item.OwnerID, item.ID, item.CreatedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workspace Base creation: %w", err)
	}
	return nil
}

// BaseByID loads one Base including its current table count.
func (repository *WorkspaceRepository) BaseByID(
	ctx context.Context,
	baseID string,
) (WorkspaceBase, error) {
	if repository == nil || repository.db == nil {
		return WorkspaceBase{}, errors.New("workspace database is required")
	}
	var item WorkspaceBase
	err := repository.db.QueryRowContext(ctx, `
		SELECT
			b.id, b.enterprise_id, b.owner_id, b.name,
			b.last_modified_at, b.last_modifier, b.created_at,
			(SELECT COUNT(*) FROM excel_structure_tables t WHERE t.base_id = b.id)
		FROM excel_workspace_bases b
		WHERE b.id = ?
	`, baseID).Scan(
		&item.ID,
		&item.EnterpriseID,
		&item.OwnerID,
		&item.Name,
		&item.LastModifiedAt,
		&item.LastModifier,
		&item.CreatedAt,
		&item.TableCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBase{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return WorkspaceBase{}, fmt.Errorf("query workspace Base: %w", err)
	}
	return item, nil
}

// RecordAccess upserts one visible Base into the user's recent list.
func (repository *WorkspaceRepository) RecordAccess(
	ctx context.Context,
	userID, baseID string,
	accessedAt time.Time,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("workspace database is required")
	}
	result, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_workspace_recent (user_id, base_id, last_accessed_at)
		SELECT ?, b.id, ?
		FROM excel_workspace_bases b
		WHERE b.id = ?
		  AND (b.owner_id = ? OR EXISTS (
			SELECT 1 FROM excel_share_collaborators c
			WHERE c.base_id = b.id AND c.user_id = ?
		  ))
		ON DUPLICATE KEY UPDATE last_accessed_at = VALUES(last_accessed_at)
	`, userID, accessedAt.UTC().UnixMilli(), baseID, userID, userID)
	if err != nil {
		return fmt.Errorf("record workspace recent access: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read workspace recent update result: %w", err)
	}
	if affected == 0 {
		return ErrWorkspaceNotFound
	}
	return nil
}

// RecentBases returns at most 50 visible Bases ordered by last access.
func (repository *WorkspaceRepository) RecentBases(
	ctx context.Context,
	userID string,
) ([]WorkspaceBase, error) {
	return repository.queryBases(ctx, userID, `
		FROM excel_workspace_recent r
		INNER JOIN excel_workspace_bases b ON b.id = r.base_id
		WHERE r.user_id = ?
		  AND (b.owner_id = ? OR EXISTS (
			SELECT 1 FROM excel_share_collaborators access
			WHERE access.base_id = b.id AND access.user_id = ?
		  ))
		ORDER BY r.last_accessed_at DESC, b.id
		LIMIT 50
	`, userID, userID, userID)
}

// CreatedBases returns Bases owned by one user.
func (repository *WorkspaceRepository) CreatedBases(
	ctx context.Context,
	userID string,
) ([]WorkspaceBase, error) {
	return repository.queryBases(ctx, userID, `
		FROM excel_workspace_bases b
		WHERE b.owner_id = ?
		ORDER BY b.last_modified_at DESC, b.id
	`, userID)
}

// SharedBases returns Bases explicitly shared with one user.
func (repository *WorkspaceRepository) SharedBases(
	ctx context.Context,
	userID string,
) ([]WorkspaceBase, error) {
	return repository.queryBases(ctx, userID, `
		FROM excel_workspace_bases b
		INNER JOIN excel_share_collaborators access
			ON access.base_id = b.id AND access.user_id = ?
		WHERE b.owner_id <> ?
		ORDER BY b.last_modified_at DESC, b.id
	`, userID, userID)
}

// StarredBases returns the user's starred, currently visible Bases.
func (repository *WorkspaceRepository) StarredBases(
	ctx context.Context,
	userID string,
) ([]WorkspaceBase, error) {
	return repository.queryBases(ctx, userID, `
		FROM excel_workspace_bases b
		INNER JOIN excel_workspace_stars star
			ON star.base_id = b.id AND star.user_id = ?
		WHERE b.owner_id = ? OR EXISTS (
			SELECT 1 FROM excel_share_collaborators access
			WHERE access.base_id = b.id AND access.user_id = ?
		)
		ORDER BY b.last_modified_at DESC, b.id
	`, userID, userID, userID)
}

// SearchBases returns Bases accessible to the user whose names match a query.
func (repository *WorkspaceRepository) SearchBases(
	ctx context.Context,
	userID, query string,
) ([]WorkspaceBase, error) {
	pattern := "%" + escapeLike(strings.TrimSpace(query)) + "%"
	return repository.queryBases(ctx, userID, `
		FROM excel_workspace_bases b
		WHERE (b.owner_id = ? OR EXISTS (
			SELECT 1 FROM excel_share_collaborators access
			WHERE access.base_id = b.id AND access.user_id = ?
		))
		  AND b.name LIKE ? ESCAPE '!'
		ORDER BY b.last_modified_at DESC, b.id
		LIMIT 100
	`, userID, userID, pattern)
}

// AccessRole returns owner or the explicit collaborator role for a Base.
func (repository *WorkspaceRepository) AccessRole(
	ctx context.Context,
	userID, baseID string,
) (string, error) {
	if repository == nil || repository.db == nil {
		return "", errors.New("workspace database is required")
	}
	var role string
	err := repository.db.QueryRowContext(ctx, `
		SELECT CASE
			WHEN b.owner_id = ? THEN 'owner'
			ELSE COALESCE((
				SELECT c.role FROM excel_share_collaborators c
				WHERE c.base_id = b.id AND c.user_id = ?
			), '')
		END
		FROM excel_workspace_bases b
		WHERE b.id = ?
	`, userID, userID, baseID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrWorkspaceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query workspace Base access role: %w", err)
	}
	return role, nil
}

// BaseNameExists checks an exact Base name within one enterprise.
func (repository *WorkspaceRepository) BaseNameExists(
	ctx context.Context,
	enterpriseID, name, exceptID string,
) (bool, error) {
	if repository == nil || repository.db == nil {
		return false, errors.New("workspace database is required")
	}
	var count int
	if err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM excel_workspace_bases
		WHERE enterprise_id = ? AND BINARY name = ? AND id <> ?
	`, enterpriseID, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("check workspace Base name: %w", err)
	}
	return count > 0, nil
}

// TableIDsForBase returns the structure module's table identities for cleanup.
func (repository *WorkspaceRepository) TableIDsForBase(
	ctx context.Context,
	baseID string,
) ([]string, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("workspace database is required")
	}
	rows, err := repository.db.QueryContext(ctx, `
		SELECT id FROM excel_structure_tables
		WHERE base_id = ?
		ORDER BY position, created_at, id
	`, baseID)
	if err != nil {
		return nil, fmt.Errorf("query Base table ids: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan Base table id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Base table ids: %w", err)
	}
	return ids, nil
}

// SetStar idempotently sets the user's star state for one Base.
func (repository *WorkspaceRepository) SetStar(
	ctx context.Context,
	userID, baseID string,
	starred bool,
	now time.Time,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("workspace database is required")
	}
	if starred {
		if _, err := repository.db.ExecContext(ctx, `
			INSERT IGNORE INTO excel_workspace_stars (base_id, user_id, created_at)
			VALUES (?, ?, ?)
		`, baseID, userID, now.UTC().UnixMilli()); err != nil {
			return fmt.Errorf("star workspace Base: %w", err)
		}
		return nil
	}
	if _, err := repository.db.ExecContext(ctx, `
		DELETE FROM excel_workspace_stars WHERE base_id = ? AND user_id = ?
	`, baseID, userID); err != nil {
		return fmt.Errorf("unstar workspace Base: %w", err)
	}
	return nil
}

// RenameBase updates one Base name and modification metadata.
func (repository *WorkspaceRepository) RenameBase(
	ctx context.Context,
	baseID, name, modifier string,
	modifiedAt time.Time,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("workspace database is required")
	}
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_workspace_bases
		SET name = ?, last_modified_at = ?, last_modifier = ?
		WHERE id = ?
	`, name, modifiedAt.UTC().UnixMilli(), modifier, baseID)
	if err != nil {
		return fmt.Errorf("rename workspace Base: %w", err)
	}
	return requireWorkspaceRow(result)
}

// TransferBaseTx changes Base ownership without touching stars or recents.
func (repository *WorkspaceRepository) TransferBaseTx(
	ctx context.Context,
	tx *sql.Tx,
	baseID, newOwnerID, modifier string,
	modifiedAt time.Time,
) error {
	if tx == nil {
		return errors.New("workspace transaction is required")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_workspace_bases
		SET owner_id = ?, last_modified_at = ?, last_modifier = ?
		WHERE id = ?
	`, newOwnerID, modifiedAt.UTC().UnixMilli(), modifier, baseID)
	if err != nil {
		return fmt.Errorf("transfer workspace Base ownership: %w", err)
	}
	return requireWorkspaceRow(result)
}

// DeleteBase removes one Base and its workspace-owned list state.
func (repository *WorkspaceRepository) DeleteBase(
	ctx context.Context,
	baseID string,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("workspace database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workspace Base deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := deleteBaseRows(ctx, tx, baseID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workspace Base deletion: %w", err)
	}
	return nil
}

// DeleteBaseTx removes one Base and its workspace-owned state in the caller's
// transaction.
func (repository *WorkspaceRepository) DeleteBaseTx(
	ctx context.Context,
	tx *sql.Tx,
	baseID string,
) error {
	if repository == nil {
		return errors.New("workspace repository is required")
	}
	return deleteBaseRows(ctx, tx, baseID)
}

func upsertRecent(
	ctx context.Context,
	tx *sql.Tx,
	userID, baseID string,
	accessedAt int64,
) error {
	if tx == nil {
		return errors.New("workspace transaction is required")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_workspace_recent (user_id, base_id, last_accessed_at)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE last_accessed_at = VALUES(last_accessed_at)
	`, userID, baseID, accessedAt); err != nil {
		return fmt.Errorf("upsert workspace recent access: %w", err)
	}
	return nil
}

func (repository *WorkspaceRepository) queryBases(
	ctx context.Context,
	userID, suffix string,
	args ...any,
) ([]WorkspaceBase, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("workspace database is required")
	}
	query := `
		SELECT
			b.id, b.enterprise_id, b.owner_id, b.name,
			b.last_modified_at, b.last_modifier, b.created_at,
			(SELECT COUNT(*) FROM excel_structure_tables t WHERE t.base_id = b.id),
			CASE
				WHEN b.owner_id = ? THEN 'owner'
				ELSE COALESCE((
					SELECT c.role FROM excel_share_collaborators c
					WHERE c.base_id = b.id AND c.user_id = ?
				), '')
			END,
			EXISTS (
				SELECT 1 FROM excel_workspace_stars s
				WHERE s.base_id = b.id AND s.user_id = ?
			)
	` + suffix
	queryArgs := make([]any, 0, len(args)+3)
	queryArgs = append(queryArgs, userID, userID, userID)
	queryArgs = append(queryArgs, args...)
	rows, err := repository.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query workspace Bases: %w", err)
	}
	defer rows.Close()
	items := make([]WorkspaceBase, 0)
	for rows.Next() {
		var item WorkspaceBase
		if err := rows.Scan(
			&item.ID,
			&item.EnterpriseID,
			&item.OwnerID,
			&item.Name,
			&item.LastModifiedAt,
			&item.LastModifier,
			&item.CreatedAt,
			&item.TableCount,
			&item.MyRole,
			&item.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan workspace Base: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspace Bases: %w", err)
	}
	return items, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value)
}

func requireWorkspaceRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read workspace row count: %w", err)
	}
	if affected == 0 {
		return ErrWorkspaceNotFound
	}
	return nil
}

func deleteBaseRows(ctx context.Context, tx *sql.Tx, baseID string) error {
	if tx == nil {
		return errors.New("workspace transaction is required")
	}
	for _, table := range []string{
		"excel_workspace_stars",
		"excel_workspace_recent",
	} {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM `+table+` WHERE base_id = ?`,
			baseID,
		); err != nil {
			return fmt.Errorf("delete workspace Base state from %s: %w", table, err)
		}
	}
	result, err := tx.ExecContext(ctx,
		`DELETE FROM excel_workspace_bases WHERE id = ?`,
		baseID,
	)
	if err != nil {
		return fmt.Errorf("delete workspace Base: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read workspace Base delete result: %w", err)
	}
	if affected == 0 {
		return baseErrWorkspaceNotFound()
	}
	return nil
}

func baseErrWorkspaceNotFound() error {
	return ErrWorkspaceNotFound
}
