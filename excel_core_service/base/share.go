// base/share.go：对应 excel_share 表的数据操作（A 模块仅操作 A 模块表，跨模块在逻辑层拆分/聚合）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// ErrShareCollaboratorExists reports a duplicate Base collaborator.
var ErrShareCollaboratorExists = errors.New("share collaborator already exists")

// ShareCollaborator is the persistence view of one explicit Base grant.
type ShareCollaborator struct {
	BaseID    string
	UserID    string
	Role      string
	Internal  bool
	Source    string
	GrantedAt int64
}

// ShareLink is the persistence view of one Base share-link configuration.
type ShareLink struct {
	BaseID    string
	Token     string
	Scope     string
	Role      string
	Enabled   bool
	URL       string
	UpdatedAt int64
}

// ShareRepository owns SQL access to excel_share_* tables.
type ShareRepository struct {
	db *sql.DB
}

// NewShareRepository creates the share persistence adapter.
func NewShareRepository(db *sql.DB) *ShareRepository {
	return &ShareRepository{db: db}
}

// BeginTx starts one transaction spanning share data and its outbox event.
func (repository *ShareRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("share database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin share transaction: %w", err)
	}
	return tx, nil
}

// Collaborator loads one explicit Base grant.
func (repository *ShareRepository) Collaborator(
	ctx context.Context,
	baseID, userID string,
) (ShareCollaborator, error) {
	const query = `
		SELECT base_id, user_id, role, internal, source, granted_at
		FROM excel_share_collaborators
		WHERE base_id = ? AND user_id = ?`
	var collaborator ShareCollaborator
	err := repository.db.QueryRowContext(ctx, query, baseID, userID).Scan(
		&collaborator.BaseID,
		&collaborator.UserID,
		&collaborator.Role,
		&collaborator.Internal,
		&collaborator.Source,
		&collaborator.GrantedAt,
	)
	if err != nil {
		return ShareCollaborator{}, fmt.Errorf("query share collaborator: %w", err)
	}
	return collaborator, nil
}

// ListCollaborators returns every explicit grant for one Base.
func (repository *ShareRepository) ListCollaborators(
	ctx context.Context,
	baseID string,
) ([]ShareCollaborator, error) {
	const query = `
		SELECT base_id, user_id, role, internal, source, granted_at
		FROM excel_share_collaborators
		WHERE base_id = ?
		ORDER BY granted_at, user_id`
	return repository.queryShareCollaborators(ctx, query, baseID)
}

// ListCollaboratorsForUpdate locks every explicit grant for a guarded mutation.
func (repository *ShareRepository) ListCollaboratorsForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	baseID string,
) ([]ShareCollaborator, error) {
	if tx == nil {
		return nil, errors.New("share transaction is required")
	}
	const query = `
		SELECT base_id, user_id, role, internal, source, granted_at
		FROM excel_share_collaborators
		WHERE base_id = ?
		ORDER BY granted_at, user_id
		FOR UPDATE`
	rows, err := tx.QueryContext(ctx, query, baseID)
	if err != nil {
		return nil, fmt.Errorf("lock share collaborators: %w", err)
	}
	defer rows.Close()
	return scanShareCollaborators(rows)
}

// InsertCollaborator creates one explicit Base grant.
func (repository *ShareRepository) InsertCollaborator(
	ctx context.Context,
	tx *sql.Tx,
	collaborator ShareCollaborator,
) error {
	if tx == nil {
		return errors.New("share transaction is required")
	}
	const query = `
		INSERT INTO excel_share_collaborators
			(base_id, user_id, role, internal, source, granted_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ExecContext(
		ctx,
		query,
		collaborator.BaseID,
		collaborator.UserID,
		collaborator.Role,
		collaborator.Internal,
		collaborator.Source,
		collaborator.GrantedAt,
	)
	if err == nil {
		return nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrShareCollaboratorExists
	}
	return fmt.Errorf("insert share collaborator: %w", err)
}

// UpdateCollaboratorRole changes one explicit Base role.
func (repository *ShareRepository) UpdateCollaboratorRole(
	ctx context.Context,
	tx *sql.Tx,
	baseID, userID, role string,
) error {
	if tx == nil {
		return errors.New("share transaction is required")
	}
	const query = `
		UPDATE excel_share_collaborators
		SET role = ?
		WHERE base_id = ? AND user_id = ?`
	_, err := tx.ExecContext(ctx, query, role, baseID, userID)
	if err != nil {
		return fmt.Errorf("update share collaborator role: %w", err)
	}
	return nil
}

// DeleteCollaborator removes one explicit Base grant.
func (repository *ShareRepository) DeleteCollaborator(
	ctx context.Context,
	tx *sql.Tx,
	baseID, userID string,
) error {
	if tx == nil {
		return errors.New("share transaction is required")
	}
	const query = `
		DELETE FROM excel_share_collaborators
		WHERE base_id = ? AND user_id = ?`
	result, err := tx.ExecContext(ctx, query, baseID, userID)
	if err != nil {
		return fmt.Errorf("delete share collaborator: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read share collaborator delete: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (repository *ShareRepository) queryShareCollaborators(
	ctx context.Context,
	query string,
	args ...any,
) ([]ShareCollaborator, error) {
	rows, err := repository.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query share collaborators: %w", err)
	}
	defer rows.Close()
	return scanShareCollaborators(rows)
}

func scanShareCollaborators(rows *sql.Rows) ([]ShareCollaborator, error) {
	collaborators := make([]ShareCollaborator, 0)
	for rows.Next() {
		var collaborator ShareCollaborator
		if err := rows.Scan(
			&collaborator.BaseID,
			&collaborator.UserID,
			&collaborator.Role,
			&collaborator.Internal,
			&collaborator.Source,
			&collaborator.GrantedAt,
		); err != nil {
			return nil, fmt.Errorf("scan share collaborator: %w", err)
		}
		collaborators = append(collaborators, collaborator)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate share collaborators: %w", err)
	}
	return collaborators, nil
}

// LinkByToken loads the current share-link configuration for a session token.
func (repository *ShareRepository) LinkByToken(ctx context.Context, token string) (ShareLink, error) {
	const query = `
		SELECT base_id, token, scope, role, enabled, url, updated_at
		FROM excel_share_links
		WHERE token = ?`
	link, err := scanShareLink(repository.db.QueryRowContext(ctx, query, token))
	if err != nil {
		return ShareLink{}, fmt.Errorf("query share link by token: %w", err)
	}
	return link, nil
}

// LinkByBaseID loads the one share-link configuration owned by a Base.
func (repository *ShareRepository) LinkByBaseID(ctx context.Context, baseID string) (ShareLink, error) {
	const query = `
		SELECT base_id, token, scope, role, enabled, url, updated_at
		FROM excel_share_links
		WHERE base_id = ?`
	link, err := scanShareLink(repository.db.QueryRowContext(ctx, query, baseID))
	if err != nil {
		return ShareLink{}, fmt.Errorf("query share link by base: %w", err)
	}
	return link, nil
}

// LinkByBaseIDForUpdate locks the Base share-link configuration for mutation.
func (repository *ShareRepository) LinkByBaseIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	baseID string,
) (ShareLink, error) {
	if tx == nil {
		return ShareLink{}, errors.New("share transaction is required")
	}
	const query = `
		SELECT base_id, token, scope, role, enabled, url, updated_at
		FROM excel_share_links
		WHERE base_id = ?
		FOR UPDATE`
	link, err := scanShareLink(tx.QueryRowContext(ctx, query, baseID))
	if err != nil {
		return ShareLink{}, fmt.Errorf("lock share link by base: %w", err)
	}
	return link, nil
}

// LinkByTokenForUpdate locks one share-link configuration by URL token.
func (repository *ShareRepository) LinkByTokenForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	token string,
) (ShareLink, error) {
	if tx == nil {
		return ShareLink{}, errors.New("share transaction is required")
	}
	const query = `
		SELECT base_id, token, scope, role, enabled, url, updated_at
		FROM excel_share_links
		WHERE token = ?
		FOR UPDATE`
	link, err := scanShareLink(tx.QueryRowContext(ctx, query, token))
	if err != nil {
		return ShareLink{}, fmt.Errorf("lock share link by token: %w", err)
	}
	return link, nil
}

// UpsertLink writes the single share-link configuration for a Base.
func (repository *ShareRepository) UpsertLink(
	ctx context.Context,
	tx *sql.Tx,
	link ShareLink,
) error {
	if tx == nil {
		return errors.New("share transaction is required")
	}
	const query = `
		INSERT INTO excel_share_links
			(base_id, token, scope, role, enabled, url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			token = VALUES(token),
			scope = VALUES(scope),
			role = VALUES(role),
			enabled = VALUES(enabled),
			url = VALUES(url),
			updated_at = VALUES(updated_at)`
	if _, err := tx.ExecContext(
		ctx,
		query,
		link.BaseID,
		link.Token,
		link.Scope,
		link.Role,
		link.Enabled,
		link.URL,
		link.UpdatedAt,
	); err != nil {
		return fmt.Errorf("upsert share link: %w", err)
	}
	return nil
}

// ListOpenLinks returns all currently enabled share links in stable Base order.
func (repository *ShareRepository) ListOpenLinks(ctx context.Context) ([]ShareLink, error) {
	const query = `
		SELECT base_id, token, scope, role, enabled, url, updated_at
		FROM excel_share_links
		WHERE enabled = 1
		ORDER BY base_id`
	rows, err := repository.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query open share links: %w", err)
	}
	defer rows.Close()
	links := make([]ShareLink, 0)
	for rows.Next() {
		link, err := scanShareLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan open share link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open share links: %w", err)
	}
	return links, nil
}

type shareLinkScanner interface {
	Scan(dest ...any) error
}

func scanShareLink(scanner shareLinkScanner) (ShareLink, error) {
	var link ShareLink
	err := scanner.Scan(
		&link.BaseID,
		&link.Token,
		&link.Scope,
		&link.Role,
		&link.Enabled,
		&link.URL,
		&link.UpdatedAt,
	)
	return link, err
}
