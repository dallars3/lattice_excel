// iam/member_role_provider.go：向 share 暴露只读的有效成员角色。
package iam

import (
	"context"
	"database/sql"
	"errors"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
)

// MemberRoleProvider implements the share member-role seam without importing share.
type MemberRoleProvider struct {
	repository *base.IAMRepository
}

// NewMemberRoleProvider creates the read-only share adapter.
func NewMemberRoleProvider(repository *base.IAMRepository) *MemberRoleProvider {
	return &MemberRoleProvider{repository: repository}
}

// Role returns only active membership roles; inactive or absent members are unspecified.
func (provider *MemberRoleProvider) Role(
	ctx context.Context,
	enterpriseID, userID string,
) (iamv1.MemberRole, error) {
	if provider == nil || provider.repository == nil {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, errors.New("IAM member repository is required")
	}
	role, err := provider.repository.ActiveMemberRole(ctx, enterpriseID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, nil
	}
	if err != nil {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, err
	}
	return memberRoleEnum(role), nil
}
