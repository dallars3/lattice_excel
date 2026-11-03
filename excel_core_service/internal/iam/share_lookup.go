// iam/share_lookup.go：向 share 暴露用户名到账号及内外身份的只读适配。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"excel_core_service/base"
)

// ShareUserLookup adapts IAM account persistence to the share UserLookup seam.
type ShareUserLookup struct {
	repository *base.IAMRepository
}

// NewShareUserLookup creates the share-facing IAM account lookup.
func NewShareUserLookup(repository *base.IAMRepository) *ShareUserLookup {
	return &ShareUserLookup{repository: repository}
}

// Lookup returns the immutable user ID and current internal flag for a username.
func (lookup *ShareUserLookup) Lookup(
	ctx context.Context,
	username string,
) (string, bool, error) {
	user, err := lookup.userByUsername(ctx, username)
	if err != nil {
		return "", false, err
	}
	return user.ID, user.Internal, nil
}

// LookupForEnterprise reports an account as internal only within the Base's enterprise.
func (lookup *ShareUserLookup) LookupForEnterprise(
	ctx context.Context,
	enterpriseID, username string,
) (string, bool, error) {
	user, err := lookup.userByUsername(ctx, username)
	if err != nil {
		return "", false, err
	}
	internal := enterpriseID != "" && user.EnterpriseID == enterpriseID
	return user.ID, internal, nil
}

func (lookup *ShareUserLookup) userByUsername(
	ctx context.Context,
	username string,
) (base.IAMUser, error) {
	if lookup == nil || lookup.repository == nil {
		return base.IAMUser{}, errors.New("IAM share lookup repository is required")
	}
	user, err := lookup.repository.UserByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return base.IAMUser{}, sql.ErrNoRows
	}
	if err != nil {
		return base.IAMUser{}, err
	}
	return user, nil
}
