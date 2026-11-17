// records/base_read_access.go：把 share.CheckAccess 适配为记录域的 Base 级授权
// seam（ADR 0004）。外部协作者（无企业归属或归属其它企业）的租户边界是 Base
// 级授权：H-48/H-51 已由网关裁决，H-49 等会话级路由网关无法按 base_id 裁决，
// 记录域解析出表行（即 Base）后必须经此 seam 做同一裁决，避免凭 record_id
// 读取未授权 Base 的数据；seam 未装配时对外部协作者 fail-closed（3001）。
package records

import (
	"context"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
	"excel_core_service/internal/share"
)

// BaseReadAccess 是记录域对外部协作者的 Base 级授权 seam。生产实现由
// share.CheckAccess 适配（本文件）；测试注入 fake。内部成员（principal 企业
// 与数据表所属企业一致）不经过此 seam，保持既有企业租户断言。
type BaseReadAccess interface {
	AllowRead(ctx context.Context, principal *commonv1.Principal, baseID string) error
}

type shareBaseReadAccess struct {
	service *share.Service
}

// NewShareBaseAccess creates the records Base-authorization adapter backed by share.
func NewShareBaseAccess(service *share.Service) BaseReadAccess {
	return &shareBaseReadAccess{service: service}
}

// AllowRead 复用 share.CheckAccess 的 VIEW 裁决（携带 LinkContext）；拒绝一律
// 3001（fail-closed），与结构域 ReadAccess、ADR 0003 的越权错误码口径一致。
func (access *shareBaseReadAccess) AllowRead(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) error {
	if access == nil || access.service == nil || principal == nil || baseID == "" {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	response, err := access.service.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal:   principal,
		BaseId:      baseID,
		Action:      sharev1.Action_ACTION_VIEW,
		LinkContext: principal.GetLinkContext(),
	})
	if err != nil {
		return err
	}
	if !response.GetAllow() {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	return nil
}
