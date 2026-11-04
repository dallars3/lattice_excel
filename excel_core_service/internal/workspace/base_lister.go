// workspace/base_lister.go：为 iam 注销清理提供企业维度 Base 清单。
package workspace

import (
	"context"

	"excel_core_service/base"
)

// BaseLister adapts WorkspaceRepository to iam.BaseLister.
type BaseLister struct {
	repository *base.WorkspaceRepository
}

// NewBaseLister constructs the dissolution cleanup Base lister.
func NewBaseLister(repository *base.WorkspaceRepository) *BaseLister {
	return &BaseLister{repository: repository}
}

// ListBases returns all Base ids owned by one enterprise.
func (lister *BaseLister) ListBases(ctx context.Context, enterpriseID string) ([]string, error) {
	return lister.repository.BaseIDsByEnterprise(ctx, enterpriseID)
}
