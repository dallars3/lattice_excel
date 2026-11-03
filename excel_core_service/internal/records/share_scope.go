// records/share_scope.go：将 share 的有效 Base 角色映射为记录行级范围。
package records

import (
	"context"
	"errors"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/internal/share"
)

type shareRowScopeProvider struct {
	service *share.Service
}

// NewShareRowScopeProvider creates the records row-scope adapter backed by share.
func NewShareRowScopeProvider(service *share.Service) RowScopeProvider {
	return &shareRowScopeProvider{service: service}
}

// RowScope returns own-row visibility only for the effective filler role.
func (provider *shareRowScopeProvider) RowScope(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) (string, error) {
	if provider == nil || provider.service == nil || principal == nil {
		return "", errors.New("share row-scope provider is not configured")
	}
	response, err := provider.service.ResolveRole(ctx, &sharev1.ResolveRoleRequest{
		Principal:   principal,
		BaseId:      baseID,
		LinkContext: principal.GetLinkContext(),
	})
	if err != nil {
		return "", err
	}
	if response.GetRole() == sharev1.Role_ROLE_FILLER {
		return rowScopeOwn, nil
	}
	return rowScopeAll, nil
}
