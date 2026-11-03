// workspace/share_adapter.go：向 share 暴露 Base 所属企业与 Base 所有者的只读适配。
package workspace

import (
	"context"
	"errors"

	"excel_core_service/base"
)

// ShareBaseResolver adapts workspace Base persistence to share.BaseResolver.
type ShareBaseResolver struct {
	repository *base.WorkspaceRepository
}

// NewShareBaseResolver creates the share-facing workspace Base resolver.
func NewShareBaseResolver(repository *base.WorkspaceRepository) *ShareBaseResolver {
	return &ShareBaseResolver{repository: repository}
}

// EnterpriseID returns the enterprise that owns a Base.
func (resolver *ShareBaseResolver) EnterpriseID(
	ctx context.Context,
	baseID string,
) (string, error) {
	if resolver == nil || resolver.repository == nil {
		return "", errors.New("workspace share resolver repository is required")
	}
	item, err := resolver.repository.BaseByID(ctx, baseID)
	if err != nil {
		return "", err
	}
	return item.EnterpriseID, nil
}

// ShareBaseOwnerProvider adapts workspace Base persistence to share.BaseOwnerProvider.
type ShareBaseOwnerProvider struct {
	repository *base.WorkspaceRepository
}

// NewShareBaseOwnerProvider creates the share-facing Base owner provider.
func NewShareBaseOwnerProvider(repository *base.WorkspaceRepository) *ShareBaseOwnerProvider {
	return &ShareBaseOwnerProvider{repository: repository}
}

// OwnerID returns the workspace owner of a Base.
func (provider *ShareBaseOwnerProvider) OwnerID(
	ctx context.Context,
	baseID string,
) (string, error) {
	if provider == nil || provider.repository == nil {
		return "", errors.New("workspace share owner repository is required")
	}
	item, err := provider.repository.BaseByID(ctx, baseID)
	if err != nil {
		return "", err
	}
	return item.OwnerID, nil
}
