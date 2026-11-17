// share/list_open_links_by_enterprise.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ListOpenLinksByEnterprise lists anyone links governed by the caller's
// enterprise. Entries carry base_name/manager metadata（H-71：管理员识别外链
// 归属与责任人）；条目以 base_id 寻址，与 H-72 强关同一寻址口径。
func (service *Service) ListOpenLinksByEnterprise(
	ctx context.Context,
	request *sharev1.ListOpenLinksByEnterpriseRequest,
) (*sharev1.ListOpenLinksByEnterpriseResponse, error) {
	if request == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if principal.GetEnterpriseId() == "" {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	memberRole, err := service.members.Role(ctx, principal.GetEnterpriseId(), principal.GetUserId())
	if err != nil ||
		(memberRole != iamv1.MemberRole_MEMBER_ROLE_OWNER &&
			memberRole != iamv1.MemberRole_MEMBER_ROLE_MANAGER) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if service.baseNames == nil || service.baseOwner == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	links, err := service.repository.ListOpenLinks(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	limit := int(request.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cursor := request.GetCursor()
	matching := make([]base.ShareLink, 0, len(links))
	for _, link := range links {
		if link.Scope != "anyone" || link.BaseID <= cursor {
			continue
		}
		enterpriseID, err := service.bases.EnterpriseID(ctx, link.BaseID)
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		if enterpriseID != principal.GetEnterpriseId() {
			continue
		}
		matching = append(matching, link)
	}
	hasMore := len(matching) > limit
	page := matching
	if hasMore {
		page = matching[:limit]
	}
	linkList, err := service.openLinkEntries(ctx, page)
	if err != nil {
		return nil, err
	}
	response := &sharev1.ListOpenLinksByEnterpriseResponse{
		LinkList: linkList,
		HasMore:  hasMore,
	}
	if hasMore && len(page) > 0 {
		response.NextCursor = page[len(page)-1].BaseID
	}
	return response, nil
}

// openLinkEntries assembles one H-71 row per link：条目 base_id 与 H-72 强关寻址
// 一致；base_name 取 Base 名称，manager 取 Base 可管理者（所有者）用户名，身份
// 缺失时兜底 user_id 保证非空（账号注销等边缘）。
func (service *Service) openLinkEntries(
	ctx context.Context,
	links []base.ShareLink,
) ([]*sharev1.OpenLink, error) {
	baseNames := make(map[string]string, len(links))
	ownerIDs := make([]string, 0, len(links))
	owners := make(map[string]string, len(links))
	for _, link := range links {
		name, err := service.baseNames.BaseName(ctx, link.BaseID)
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		baseNames[link.BaseID] = name
		ownerID, err := service.baseOwner.OwnerID(ctx, link.BaseID)
		if err != nil || ownerID == "" {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		owners[link.BaseID] = ownerID
		ownerIDs = append(ownerIDs, ownerID)
	}
	profiles, err := service.profilesFor(ctx, ownerIDs)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	entries := make([]*sharev1.OpenLink, 0, len(links))
	for _, link := range links {
		ownerID := owners[link.BaseID]
		manager := ownerID
		if profile := profiles[ownerID]; profile != nil && profile.GetUsername() != "" {
			manager = profile.GetUsername()
		}
		entries = append(entries, &sharev1.OpenLink{
			BaseId:   link.BaseID,
			BaseName: baseNames[link.BaseID],
			Manager:  manager,
			Role:     protoRole(link.Role),
			OpenedAt: link.UpdatedAt,
		})
	}
	return entries, nil
}
