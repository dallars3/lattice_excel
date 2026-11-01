// iam/list_members.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"strings"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// ListMembers returns one enterprise member keyset page with optional search.
func (service *IamService) ListMembers(
	ctx context.Context,
	request *iamv1.ListMembersRequest,
) (*iamv1.ListMembersResponse, error) {
	if request == nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	query := strings.TrimSpace(request.GetQ())
	if len([]rune(query)) > 50 {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	operator, err := service.activeMember(ctx, principal)
	if err != nil {
		return nil, err
	}
	if !canManageMembers(operator) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	limit := int(request.GetLimit())
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	members, err := service.repository.ListMembers(
		ctx,
		operator.EnterpriseID,
		request.GetCursor(),
		query,
		limit+1,
	)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}
	response := &iamv1.ListMembersResponse{
		MemberList: make([]*iamv1.Member, 0, len(members)),
		HasMore:    hasMore,
	}
	for _, member := range members {
		response.MemberList = append(response.MemberList, protoMember(member))
	}
	if hasMore && len(members) > 0 {
		response.NextCursor = members[len(members)-1].UserID
	}
	return response, nil
}
