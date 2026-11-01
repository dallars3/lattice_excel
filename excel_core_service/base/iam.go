// base/iam.go：对应 excel_iam 表的数据操作（A 模块仅操作 A 模块表，跨模块在逻辑层拆分/聚合）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// ErrUsernameExists reports a case-insensitive username uniqueness conflict.
var ErrUsernameExists = errors.New("username already exists")

// IAMUser is the persistence view of an account.
type IAMUser struct {
	ID           string
	Username     string
	DisplayName  string
	AvatarURI    string
	PasswordHash string
	EnterpriseID string
	Internal     bool
	LinkBaseID   string
	LinkToken    string
	LinkRole     string
	CreatedAt    int64
}

// IAMEnterprise is the persistence view of an enterprise.
type IAMEnterprise struct {
	ID          string
	Name        string
	OwnerID     string
	MemberCount int32
	CreatedAt   int64
}

// IAMRepository owns SQL access to excel_iam_* tables.
type IAMRepository struct {
	db *sql.DB
}

// NewIAMRepository creates the IAM persistence adapter.
func NewIAMRepository(db *sql.DB) *IAMRepository {
	return &IAMRepository{db: db}
}

// CreateUser creates an account without an enterprise membership.
func (repository *IAMRepository) CreateUser(ctx context.Context, user IAMUser) (IAMUser, error) {
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return IAMUser{}, fmt.Errorf("begin IAM account creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertIAMUser(ctx, tx, user); err != nil {
		return IAMUser{}, err
	}
	if err := tx.Commit(); err != nil {
		return IAMUser{}, fmt.Errorf("commit IAM account creation: %w", err)
	}
	return user, nil
}

// UserByUsername loads an account using the username's case-insensitive collation.
func (repository *IAMRepository) UserByUsername(ctx context.Context, username string) (IAMUser, error) {
	return repository.userByUniqueField(ctx, "username", username)
}

// UserByID loads an account by its immutable ULID.
func (repository *IAMRepository) UserByID(ctx context.Context, userID string) (IAMUser, error) {
	return repository.userByUniqueField(ctx, "id", userID)
}

func (repository *IAMRepository) userByUniqueField(ctx context.Context, field, value string) (IAMUser, error) {
	query := `
		SELECT id, username, display_name, avatar_uri, password_hash,
		       COALESCE(enterprise_id, ''), internal,
		       COALESCE(link_base_id, ''), COALESCE(link_token, ''), COALESCE(link_role, ''),
		       created_at
		FROM excel_iam_users
		WHERE ` + field + ` = ?`
	var user IAMUser
	err := repository.db.QueryRowContext(ctx, query, value).Scan(
		&user.ID,
		&user.Username,
		&user.DisplayName,
		&user.AvatarURI,
		&user.PasswordHash,
		&user.EnterpriseID,
		&user.Internal,
		&user.LinkBaseID,
		&user.LinkToken,
		&user.LinkRole,
		&user.CreatedAt,
	)
	if err != nil {
		return IAMUser{}, fmt.Errorf("query IAM user by %s: %w", field, err)
	}
	return user, nil
}

// EnterpriseAndRoleByUserID loads the user's active enterprise and membership role.
func (repository *IAMRepository) EnterpriseAndRoleByUserID(
	ctx context.Context,
	userID string,
) (IAMEnterprise, string, error) {
	const query = `
		SELECT e.id, e.name, e.owner_id, e.member_count, e.created_at, m.role
		FROM excel_iam_members m
		JOIN excel_iam_enterprises e ON e.id = m.enterprise_id
		WHERE m.user_id = ?`
	var enterprise IAMEnterprise
	var role string
	err := repository.db.QueryRowContext(ctx, query, userID).Scan(
		&enterprise.ID,
		&enterprise.Name,
		&enterprise.OwnerID,
		&enterprise.MemberCount,
		&enterprise.CreatedAt,
		&role,
	)
	if err != nil {
		return IAMEnterprise{}, "", fmt.Errorf("query IAM enterprise for user: %w", err)
	}
	return enterprise, role, nil
}

// UpdateUserProfile changes the mutable profile fields of one account.
func (repository *IAMRepository) UpdateUserProfile(
	ctx context.Context,
	userID, displayName, avatarURI string,
) error {
	const query = `UPDATE excel_iam_users SET display_name = ?, avatar_uri = ? WHERE id = ?`
	if _, err := repository.db.ExecContext(ctx, query, displayName, avatarURI, userID); err != nil {
		return fmt.Errorf("update IAM user profile: %w", err)
	}
	return nil
}

// UpdatePasswordHash replaces the bcrypt credential of one account.
func (repository *IAMRepository) UpdatePasswordHash(ctx context.Context, userID, passwordHash string) error {
	const query = `UPDATE excel_iam_users SET password_hash = ? WHERE id = ?`
	result, err := repository.db.ExecContext(ctx, query, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("update IAM password hash: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read IAM password update result: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CreateUserWithEnterprise atomically creates an enterprise owner account.
func (repository *IAMRepository) CreateUserWithEnterprise(
	ctx context.Context,
	user IAMUser,
	enterprise IAMEnterprise,
) (IAMUser, IAMEnterprise, error) {
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return IAMUser{}, IAMEnterprise{}, fmt.Errorf("begin IAM registration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertIAMUser(ctx, tx, user); err != nil {
		return IAMUser{}, IAMEnterprise{}, err
	}
	const insertEnterprise = `
		INSERT INTO excel_iam_enterprises (id, name, owner_id, member_count, created_at)
		VALUES (?, ?, ?, ?, ?)`
	if _, err := tx.ExecContext(
		ctx,
		insertEnterprise,
		enterprise.ID,
		enterprise.Name,
		enterprise.OwnerID,
		enterprise.MemberCount,
		enterprise.CreatedAt,
	); err != nil {
		return IAMUser{}, IAMEnterprise{}, fmt.Errorf("insert IAM enterprise: %w", err)
	}
	const setEnterprise = `UPDATE excel_iam_users SET enterprise_id = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, setEnterprise, enterprise.ID, user.ID); err != nil {
		return IAMUser{}, IAMEnterprise{}, fmt.Errorf("set IAM user enterprise: %w", err)
	}
	const insertMember = `
		INSERT INTO excel_iam_members (enterprise_id, user_id, role, status, joined_at)
		VALUES (?, ?, 'owner', 'active', ?)`
	if _, err := tx.ExecContext(ctx, insertMember, enterprise.ID, user.ID, user.CreatedAt); err != nil {
		return IAMUser{}, IAMEnterprise{}, fmt.Errorf("insert IAM owner member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return IAMUser{}, IAMEnterprise{}, fmt.Errorf("commit IAM registration: %w", err)
	}

	user.EnterpriseID = enterprise.ID
	return user, enterprise, nil
}

func insertIAMUser(ctx context.Context, tx *sql.Tx, user IAMUser) error {
	const query = `
		INSERT INTO excel_iam_users (
			id, username, display_name, avatar_uri, password_hash,
			enterprise_id, internal, link_base_id, link_token, link_role, created_at
		) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?)`
	_, err := tx.ExecContext(
		ctx,
		query,
		user.ID,
		user.Username,
		user.DisplayName,
		user.AvatarURI,
		user.PasswordHash,
		user.EnterpriseID,
		user.Internal,
		user.LinkBaseID,
		user.LinkToken,
		user.LinkRole,
		user.CreatedAt,
	)
	if err == nil {
		return nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrUsernameExists
	}
	return fmt.Errorf("insert IAM user: %w", err)
}
