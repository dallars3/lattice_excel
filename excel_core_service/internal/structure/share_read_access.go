// structure/share_read_access.go：把 share.CheckAccess 适配为结构域只读 seam。
package structure

import (
	"context"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
	"excel_core_service/internal/share"
)

type shareReadAccess struct {
	service *share.Service
}

// NewShareReadAccess creates the Base read-authorization adapter backed by share.
func NewShareReadAccess(service *share.Service) ReadAccess {
	return &shareReadAccess{service: service}
}

// AllowRead 复用 share.CheckAccess 的 VIEW 裁决；未装配或拒绝一律 3001（fail-closed）。
func (access *shareReadAccess) AllowRead(
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
