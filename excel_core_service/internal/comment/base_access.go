// comment/base_access.go：把 share.ResolveRole 适配为评论域的 Base 角色 seam
// （spec §4 评论权限矩阵）。与 records 的 BaseReadAccess、structure 的
// ReadAccess 同构：内部成员经成员/所有者/协作者裁决产出角色，外部协作者按
// 协作者条目与链接角色封顶产出；seam 未装配时按无角色处理（fail-closed，
// 评论写路径一律 3001）。
package comment

import (
	"context"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/internal/share"
)

// BaseRoleAccess resolves the caller's effective Base role for comment decisions.
type BaseRoleAccess interface {
	Role(ctx context.Context, principal *commonv1.Principal, baseID string) (sharev1.Role, error)
}

type shareRoleAccess struct {
	service *share.Service
}

// NewShareRoleAccess creates the comment Base-role adapter backed by share.
func NewShareRoleAccess(service *share.Service) BaseRoleAccess {
	return &shareRoleAccess{service: service}
}

// Role 复用 share.ResolveRole 的唯一角色裁决（携带 LinkContext）。seam 未装配
// 时返回无角色（fail-closed）；share 裁决的基础设施错误原样上抛，不吞成权限码。
func (access *shareRoleAccess) Role(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) (sharev1.Role, error) {
	if access == nil || access.service == nil || principal == nil || baseID == "" {
		return sharev1.Role_ROLE_UNSPECIFIED, nil
	}
	response, err := access.service.ResolveRole(ctx, &sharev1.ResolveRoleRequest{
		Principal:   principal,
		BaseId:      baseID,
		LinkContext: principal.GetLinkContext(),
	})
	if err != nil {
		return sharev1.Role_ROLE_UNSPECIFIED, err
	}
	return response.GetRole(), nil
}
