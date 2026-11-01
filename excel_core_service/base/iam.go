// base/iam.go：对应 excel_iam 表的数据操作（A 模块仅操作 A 模块表，跨模块在逻辑层拆分/聚合）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// ErrUsernameExists reports a case-insensitive username uniqueness conflict.
var ErrUsernameExists = errors.New("username already exists")

// ErrIAMInvitationExists reports a duplicate enterprise username invitation.
var ErrIAMInvitationExists = errors.New("IAM invitation already exists")

// ErrIAMMemberExists reports a duplicate account membership.
var ErrIAMMemberExists = errors.New("IAM member already exists")

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

// IAMMember is the persistence view of one enterprise membership.
type IAMMember struct {
	EnterpriseID string
	UserID       string
	Username     string
	DisplayName  string
	Role         string
	Status       string
	JoinedAt     int64
}

// IAMInvitation is the persistence view of one enterprise invitation.
type IAMInvitation struct {
	ID             string
	EnterpriseID   string
	EnterpriseName string
	Username       string
	UserID         string
	Status         string
	CreatedAt      int64
}

// IAMRepository owns SQL access to excel_iam_* tables.
type IAMRepository struct {
	db *sql.DB
}

// NewIAMRepository creates the IAM persistence adapter.
func NewIAMRepository(db *sql.DB) *IAMRepository {
	return &IAMRepository{db: db}
}

// BeginTx starts one transaction spanning IAM data and its outbox event.
func (repository *IAMRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("IAM database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin IAM transaction: %w", err)
	}
	return tx, nil
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

const iamUserSelect = `
	SELECT id, username, display_name, avatar_uri, password_hash,
	       COALESCE(enterprise_id, ''), internal,
	       COALESCE(link_base_id, ''), COALESCE(link_token, ''), COALESCE(link_role, ''),
	       created_at
	FROM excel_iam_users`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIAMUser(row rowScanner) (IAMUser, error) {
	var user IAMUser
	err := row.Scan(
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
		return IAMUser{}, err
	}
	return user, nil
}

func queryIAMEnterprise(row rowScanner) (IAMEnterprise, error) {
	var enterprise IAMEnterprise
	err := row.Scan(
		&enterprise.ID,
		&enterprise.Name,
		&enterprise.OwnerID,
		&enterprise.MemberCount,
		&enterprise.CreatedAt,
	)
	if err != nil {
		return IAMEnterprise{}, fmt.Errorf("scan IAM enterprise: %w", err)
	}
	return enterprise, nil
}

func queryIAMMember(row rowScanner) (IAMMember, error) {
	var member IAMMember
	err := row.Scan(
		&member.EnterpriseID,
		&member.UserID,
		&member.Username,
		&member.DisplayName,
		&member.Role,
		&member.Status,
		&member.JoinedAt,
	)
	if err != nil {
		return IAMMember{}, fmt.Errorf("scan IAM member: %w", err)
	}
	return member, nil
}

func scanIAMMembers(rows *sql.Rows) ([]IAMMember, error) {
	members := make([]IAMMember, 0)
	for rows.Next() {
		member, err := queryIAMMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate IAM members: %w", err)
	}
	return members, nil
}

func queryIAMInvitation(row rowScanner) (IAMInvitation, error) {
	var invitation IAMInvitation
	err := row.Scan(
		&invitation.ID,
		&invitation.EnterpriseID,
		&invitation.EnterpriseName,
		&invitation.Username,
		&invitation.UserID,
		&invitation.Status,
		&invitation.CreatedAt,
	)
	if err != nil {
		return IAMInvitation{}, fmt.Errorf("scan IAM invitation: %w", err)
	}
	return invitation, nil
}

func scanIAMInvitations(rows *sql.Rows) ([]IAMInvitation, error) {
	invitations := make([]IAMInvitation, 0)
	for rows.Next() {
		invitation, err := queryIAMInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate IAM invitations: %w", err)
	}
	return invitations, nil
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

// EnterpriseByID loads one enterprise.
func (repository *IAMRepository) EnterpriseByID(
	ctx context.Context,
	enterpriseID string,
) (IAMEnterprise, error) {
	return queryIAMEnterprise(
		repository.db.QueryRowContext(ctx,
			`SELECT id, name, owner_id, member_count, created_at
			 FROM excel_iam_enterprises WHERE id = ?`,
			enterpriseID,
		),
	)
}

// EnterpriseByIDForUpdate locks one enterprise for an ownership mutation.
func (repository *IAMRepository) EnterpriseByIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID string,
) (IAMEnterprise, error) {
	if tx == nil {
		return IAMEnterprise{}, errors.New("IAM transaction is required")
	}
	return queryIAMEnterprise(
		tx.QueryRowContext(ctx,
			`SELECT id, name, owner_id, member_count, created_at
			 FROM excel_iam_enterprises WHERE id = ? FOR UPDATE`,
			enterpriseID,
		),
	)
}

// Member loads one membership, including disabled memberships.
func (repository *IAMRepository) Member(
	ctx context.Context,
	enterpriseID, userID string,
) (IAMMember, error) {
	return queryIAMMember(
		repository.db.QueryRowContext(ctx,
			`SELECT m.enterprise_id, m.user_id, u.username, u.display_name,
			        m.role, m.status, m.joined_at
			 FROM excel_iam_members m
			 JOIN excel_iam_users u ON u.id = m.user_id
			 WHERE m.enterprise_id = ? AND m.user_id = ?`,
			enterpriseID,
			userID,
		),
	)
}

// MemberForUpdate locks one membership for a guarded mutation.
func (repository *IAMRepository) MemberForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID, userID string,
) (IAMMember, error) {
	if tx == nil {
		return IAMMember{}, errors.New("IAM transaction is required")
	}
	return queryIAMMember(
		tx.QueryRowContext(ctx,
			`SELECT m.enterprise_id, m.user_id, u.username, u.display_name,
			        m.role, m.status, m.joined_at
			 FROM excel_iam_members m
			 JOIN excel_iam_users u ON u.id = m.user_id
			 WHERE m.enterprise_id = ? AND m.user_id = ? FOR UPDATE`,
			enterpriseID,
			userID,
		),
	)
}

// MemberByUserID loads the sole membership of an account.
func (repository *IAMRepository) MemberByUserID(
	ctx context.Context,
	userID string,
) (IAMMember, error) {
	return queryIAMMember(
		repository.db.QueryRowContext(ctx,
			`SELECT m.enterprise_id, m.user_id, u.username, u.display_name,
			        m.role, m.status, m.joined_at
			 FROM excel_iam_members m
			 JOIN excel_iam_users u ON u.id = m.user_id
			 WHERE m.user_id = ?`,
			userID,
		),
	)
}

// ActiveMemberRole returns the role only for an active membership.
func (repository *IAMRepository) ActiveMemberRole(
	ctx context.Context,
	enterpriseID, userID string,
) (string, error) {
	var role string
	err := repository.db.QueryRowContext(ctx,
		`SELECT role FROM excel_iam_members
		 WHERE enterprise_id = ? AND user_id = ? AND status = 'active'`,
		enterpriseID,
		userID,
	).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("query active IAM member role: %w", err)
	}
	return role, nil
}

// ListMembers returns one keyset page ordered by immutable user ID.
func (repository *IAMRepository) ListMembers(
	ctx context.Context,
	enterpriseID, cursor, query string,
	limit int,
) ([]IAMMember, error) {
	query = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query)
	rows, err := repository.db.QueryContext(ctx,
		`SELECT m.enterprise_id, m.user_id, u.username, u.display_name,
		        m.role, m.status, m.joined_at
		 FROM excel_iam_members m
		 JOIN excel_iam_users u ON u.id = m.user_id
		 WHERE m.enterprise_id = ?
		   AND m.user_id > ?
		   AND (? = '' OR u.username LIKE CONCAT('%', ?, '%') ESCAPE '!'
		        OR u.display_name LIKE CONCAT('%', ?, '%') ESCAPE '!')
		 ORDER BY m.user_id
		 LIMIT ?`,
		enterpriseID,
		cursor,
		query,
		query,
		query,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query IAM members: %w", err)
	}
	defer rows.Close()
	return scanIAMMembers(rows)
}

// UpdateMemberStatus changes an existing membership status.
func (repository *IAMRepository) UpdateMemberStatus(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID, userID, status string,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_members SET status = ?
		 WHERE enterprise_id = ? AND user_id = ?`,
		status,
		enterpriseID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update IAM member status: %w", err)
	}
	return requireOneRow(result, "update IAM member status")
}

// UpdateMemberRole changes an existing membership role.
func (repository *IAMRepository) UpdateMemberRole(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID, userID, role string,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_members SET role = ?
		 WHERE enterprise_id = ? AND user_id = ?`,
		role,
		enterpriseID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update IAM member role: %w", err)
	}
	return requireOneRow(result, "update IAM member role")
}

// CountActiveOwners reports the owner invariant for one enterprise.
func (repository *IAMRepository) CountActiveOwners(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID string,
) (int, error) {
	if tx == nil {
		return 0, errors.New("IAM transaction is required")
	}
	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM excel_iam_members
		 WHERE enterprise_id = ? AND role = 'owner' AND status = 'active'`,
		enterpriseID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count IAM active owners: %w", err)
	}
	return count, nil
}

// SetEnterpriseOwner updates the denormalized owner ID.
func (repository *IAMRepository) SetEnterpriseOwner(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID, ownerID string,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_enterprises SET owner_id = ? WHERE id = ?`,
		ownerID,
		enterpriseID,
	)
	if err != nil {
		return fmt.Errorf("set IAM enterprise owner: %w", err)
	}
	return requireOneRow(result, "set IAM enterprise owner")
}

// ListInvitations returns one pending keyset page ordered by invitation ID.
func (repository *IAMRepository) ListInvitations(
	ctx context.Context,
	enterpriseID, cursor string,
	limit int,
) ([]IAMInvitation, error) {
	rows, err := repository.db.QueryContext(ctx,
		`SELECT i.id, i.enterprise_id, e.name, i.username,
		        COALESCE(i.user_id, ''), i.status, i.created_at
		 FROM excel_iam_invitations i
		 JOIN excel_iam_enterprises e ON e.id = i.enterprise_id
		 WHERE i.enterprise_id = ? AND i.status = 'pending' AND i.id > ?
		 ORDER BY i.id
		 LIMIT ?`,
		enterpriseID,
		cursor,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query IAM invitations: %w", err)
	}
	defer rows.Close()
	return scanIAMInvitations(rows)
}

// ListUserInvitations returns all pending invitations for one account or username.
func (repository *IAMRepository) ListUserInvitations(
	ctx context.Context,
	userID, username string,
) ([]IAMInvitation, error) {
	rows, err := repository.db.QueryContext(ctx,
		`SELECT i.id, i.enterprise_id, e.name, i.username,
		        COALESCE(i.user_id, ''), i.status, i.created_at
		 FROM excel_iam_invitations i
		 JOIN excel_iam_enterprises e ON e.id = i.enterprise_id
		 WHERE i.status = 'pending'
		   AND (i.user_id = ? OR LOWER(i.username) = LOWER(?))
		 ORDER BY i.created_at, i.id`,
		userID,
		username,
	)
	if err != nil {
		return nil, fmt.Errorf("query personal IAM invitations: %w", err)
	}
	defer rows.Close()
	return scanIAMInvitations(rows)
}

// InvitationByIDForUpdate locks one invitation for a state transition.
func (repository *IAMRepository) InvitationByIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	invitationID string,
) (IAMInvitation, error) {
	if tx == nil {
		return IAMInvitation{}, errors.New("IAM transaction is required")
	}
	return queryIAMInvitation(
		tx.QueryRowContext(ctx,
			`SELECT i.id, i.enterprise_id, e.name, i.username,
			        COALESCE(i.user_id, ''), i.status, i.created_at
			 FROM excel_iam_invitations i
			 JOIN excel_iam_enterprises e ON e.id = i.enterprise_id
			 WHERE i.id = ? FOR UPDATE`,
			invitationID,
		),
	)
}

// InvitationByEnterpriseAndUsernameForUpdate locks the unique invitation slot.
func (repository *IAMRepository) InvitationByEnterpriseAndUsernameForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID, username string,
) (IAMInvitation, error) {
	if tx == nil {
		return IAMInvitation{}, errors.New("IAM transaction is required")
	}
	return queryIAMInvitation(
		tx.QueryRowContext(ctx,
			`SELECT i.id, i.enterprise_id, e.name, i.username,
			        COALESCE(i.user_id, ''), i.status, i.created_at
			 FROM excel_iam_invitations i
			 JOIN excel_iam_enterprises e ON e.id = i.enterprise_id
			 WHERE i.enterprise_id = ? AND LOWER(i.username) = LOWER(?)
			 FOR UPDATE`,
			enterpriseID,
			username,
		),
	)
}

// InsertInvitation creates one invitation.
func (repository *IAMRepository) InsertInvitation(
	ctx context.Context,
	tx *sql.Tx,
	invitation IAMInvitation,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO excel_iam_invitations
			(id, enterprise_id, username, user_id, status, created_at)
		 VALUES (?, ?, ?, NULLIF(?, ''), ?, ?)`,
		invitation.ID,
		invitation.EnterpriseID,
		invitation.Username,
		invitation.UserID,
		invitation.Status,
		invitation.CreatedAt,
	)
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrIAMInvitationExists
		}
		return fmt.Errorf("insert IAM invitation: %w", err)
	}
	return nil
}

// ReopenInvitation moves a terminal invitation back to pending.
func (repository *IAMRepository) ReopenInvitation(
	ctx context.Context,
	tx *sql.Tx,
	invitation IAMInvitation,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_invitations
		 SET username = ?, user_id = NULLIF(?, ''), status = 'pending', created_at = ?
		 WHERE id = ?`,
		invitation.Username,
		invitation.UserID,
		invitation.CreatedAt,
		invitation.ID,
	)
	if err != nil {
		return fmt.Errorf("reopen IAM invitation: %w", err)
	}
	return requireOneRow(result, "reopen IAM invitation")
}

// UpdateInvitationStatus applies one locked state transition.
func (repository *IAMRepository) UpdateInvitationStatus(
	ctx context.Context,
	tx *sql.Tx,
	invitationID, status, userID string,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_invitations
		 SET status = ?, user_id = COALESCE(NULLIF(?, ''), user_id)
		 WHERE id = ?`,
		status,
		userID,
		invitationID,
	)
	if err != nil {
		return fmt.Errorf("update IAM invitation status: %w", err)
	}
	return requireOneRow(result, "update IAM invitation status")
}

// InsertMember creates the active membership accepted from an invitation.
func (repository *IAMRepository) InsertMember(
	ctx context.Context,
	tx *sql.Tx,
	member IAMMember,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO excel_iam_members
			(enterprise_id, user_id, role, status, joined_at)
		 VALUES (?, ?, ?, ?, ?)`,
		member.EnterpriseID,
		member.UserID,
		member.Role,
		member.Status,
		member.JoinedAt,
	)
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrIAMMemberExists
		}
		return fmt.Errorf("insert IAM member: %w", err)
	}
	return nil
}

// SetUserEnterprise assigns an account to an enterprise.
func (repository *IAMRepository) SetUserEnterprise(
	ctx context.Context,
	tx *sql.Tx,
	userID, enterpriseID string,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_users SET enterprise_id = ?, internal = 1 WHERE id = ?`,
		enterpriseID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("set IAM user enterprise: %w", err)
	}
	return requireOneRow(result, "set IAM user enterprise")
}

// IncrementEnterpriseMemberCount updates the denormalized member total.
func (repository *IAMRepository) IncrementEnterpriseMemberCount(
	ctx context.Context,
	tx *sql.Tx,
	enterpriseID string,
	delta int,
) error {
	if tx == nil {
		return errors.New("IAM transaction is required")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE excel_iam_enterprises
		 SET member_count = member_count + ?
		 WHERE id = ?`,
		delta,
		enterpriseID,
	)
	if err != nil {
		return fmt.Errorf("increment IAM enterprise member count: %w", err)
	}
	return requireOneRow(result, "increment IAM enterprise member count")
}

// UserByIDForUpdate locks one account for a membership transition.
func (repository *IAMRepository) UserByIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	userID string,
) (IAMUser, error) {
	if tx == nil {
		return IAMUser{}, errors.New("IAM transaction is required")
	}
	return scanIAMUser(tx.QueryRowContext(ctx, iamUserSelect+` WHERE id = ? FOR UPDATE`, userID))
}

// UserByUsernameForUpdate locks one account by case-insensitive username.
func (repository *IAMRepository) UserByUsernameForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	username string,
) (IAMUser, error) {
	if tx == nil {
		return IAMUser{}, errors.New("IAM transaction is required")
	}
	return scanIAMUser(tx.QueryRowContext(
		ctx,
		iamUserSelect+` WHERE username = ? FOR UPDATE`,
		username,
	))
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
