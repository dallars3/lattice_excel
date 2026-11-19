// iam/enterprise_directory.go：向 share 暴露企业成员名册与所有者的只读适配
// （票 04/N2 人员目录并集的企业侧来源）。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"excel_core_service/base"
)

// EnterpriseDirectoryProvider implements the share enterprise-directory seam
// without importing share.
type EnterpriseDirectoryProvider struct {
	repository *base.IAMRepository
}

// NewEnterpriseDirectoryProvider creates the read-only share adapter.
func NewEnterpriseDirectoryProvider(repository *base.IAMRepository) *EnterpriseDirectoryProvider {
	return &EnterpriseDirectoryProvider{repository: repository}
}

// MembersByEnterprise returns the active member roster（含所有者成员行；禁用成员
// 与停用账号不入册）。
func (provider *EnterpriseDirectoryProvider) MembersByEnterprise(
	ctx context.Context,
	enterpriseID string,
) ([]base.IAMMember, error) {
	if provider == nil || provider.repository == nil {
		return nil, errors.New("IAM member repository is required")
	}
	return provider.repository.ActiveMembers(ctx, enterpriseID)
}

// OwnerID returns the enterprise owner's user ID; a missing enterprise degrades
// to an empty owner（目录并集的所有者来源，幂等兜底不放大故障）。
func (provider *EnterpriseDirectoryProvider) OwnerID(
	ctx context.Context,
	enterpriseID string,
) (string, error) {
	if provider == nil || provider.repository == nil {
		return "", errors.New("IAM member repository is required")
	}
	enterprise, err := provider.repository.EnterpriseByID(ctx, enterpriseID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return enterprise.OwnerID, nil
}
