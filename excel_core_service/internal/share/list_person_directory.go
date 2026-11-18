// share/list_person_directory.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"sort"
	"strings"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// ListPersonDirectory returns the person-field candidate directory of one Base
// as「Base 外部协作者 ∪ 企业成员 ∪ 所有者」并集（票 04/N2），按调用者身份裁剪：
//   - 内部用户（principal 企业 = Base 企业）：企业全员（active 成员 + 所有者）
//     ∪ Base 协作者——owner-only 企业立即可选所有者与企业成员；
//   - 其余调用者（外部协作者等）：仅该 Base 协作者集合，不泄露企业全员名单。
//
// 条目形状与协作者列表对齐（id/user_id/username/display_name，显示名缺省回落
// 用户名），目录是身份目录：role/source/granted_at 恒为零值，不携带授权元数据。
// 同一人兼具多重身份（所有者+成员、成员+协作者）只出一条（按 user_id 去重）。
func (service *Service) ListPersonDirectory(
	ctx context.Context,
	request *sharev1.ListPersonDirectoryRequest,
) (*sharev1.ListPersonDirectoryResponse, error) {
	if request == nil || strings.TrimSpace(request.GetBaseId()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	decision, err := service.resolveDecision(ctx, principal, request.GetBaseId(), principal.GetLinkContext())
	if err != nil {
		return nil, err
	}
	roles, err := service.policy.current()
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if !service.decisionAllows(sharev1.Action_ACTION_VIEW, decision, roles) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}

	// 身份并集：协作者恒在册；内部调用者再并入企业全员与所有者（去重）。
	// 值为 internal 标记——企业成员/所有者恒 true；协作者沿用授权时身份，
	// 同一人现为企业成员时以成员身份为准（覆盖为 true）。
	directory := make(map[string]bool)
	collaborators, err := service.repository.ListCollaborators(ctx, request.GetBaseId())
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	for _, collaborator := range collaborators {
		directory[collaborator.UserID] = collaborator.Internal
	}
	if decision.internal {
		enterpriseID, err := service.bases.EnterpriseID(ctx, request.GetBaseId())
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		members, err := service.enterpriseDirectory.MembersByEnterprise(ctx, enterpriseID)
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		for _, member := range members {
			directory[member.UserID] = true
		}
		ownerID, err := service.enterpriseDirectory.OwnerID(ctx, enterpriseID)
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		if ownerID != "" {
			directory[ownerID] = true
		}
	}

	userIDs := make([]string, 0, len(directory))
	for userID := range directory {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	profiles, err := service.profilesFor(ctx, userIDs)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	response := &sharev1.ListPersonDirectoryResponse{
		CollaboratorList: make([]*sharev1.Collaborator, 0, len(userIDs)),
	}
	for _, userID := range userIDs {
		response.CollaboratorList = append(
			response.CollaboratorList,
			directoryEntry(userID, directory[userID], profiles),
		)
	}
	return response, nil
}

// directoryEntry builds one identity-only entry: real username/display_name from
// the account profile, falling back to user_id（账号缺失等边缘，与协作者条目口径
// 一致）；role/source/granted_at 保持零值——目录不携带授权元数据。internal 标记
// 该用户是否为 Base 所属企业的内部成员。
func directoryEntry(
	userID string,
	internal bool,
	profiles map[string]*iamv1.User,
) *sharev1.Collaborator {
	username := userID
	displayName := userID
	if profile := profiles[userID]; profile != nil {
		if profile.GetUsername() != "" {
			username = profile.GetUsername()
		}
		if profile.GetDisplayName() != "" {
			displayName = profile.GetDisplayName()
		}
	}
	return &sharev1.Collaborator{
		Id:          userID,
		UserId:      userID,
		Username:    username,
		DisplayName: displayName,
		Internal:    internal,
	}
}
