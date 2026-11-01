// share/list_open_links_by_enterprise.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// ListOpenLinksByEnterprise lists anyone links governed by the caller's enterprise.
func (service *Service) ListOpenLinksByEnterprise(
	ctx context.Context,
	request *sharev1.ListOpenLinksByEnterpriseRequest,
) (*sharev1.ListOpenLinksByEnterpriseResponse, error) {
	if request == nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if principal.GetEnterpriseId() == "" {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	memberRole, err := service.members.Role(ctx, principal.GetEnterpriseId(), principal.GetUserId())
	if err != nil ||
		(memberRole != iamv1.MemberRole_MEMBER_ROLE_OWNER &&
			memberRole != iamv1.MemberRole_MEMBER_ROLE_MANAGER) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	links, err := service.repository.ListOpenLinks(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	limit := int(request.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cursor := request.GetCursor()
	filtered := make([]*sharev1.OpenLink, 0, len(links))
	for _, link := range links {
		if link.Scope != "anyone" || link.BaseID <= cursor {
			continue
		}
		enterpriseID, err := service.bases.EnterpriseID(ctx, link.BaseID)
		if err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		if enterpriseID != principal.GetEnterpriseId() {
			continue
		}
		filtered = append(filtered, &sharev1.OpenLink{
			BaseId:   link.BaseID,
			Role:     protoRole(link.Role),
			OpenedAt: link.UpdatedAt,
		})
	}
	hasMore := len(filtered) > limit
	if hasMore {
		filtered = filtered[:limit]
	}
	response := &sharev1.ListOpenLinksByEnterpriseResponse{
		LinkList: filtered,
		HasMore:  hasMore,
	}
	if hasMore {
		response.NextCursor = filtered[len(filtered)-1].GetBaseId()
	}
	return response, nil
}
