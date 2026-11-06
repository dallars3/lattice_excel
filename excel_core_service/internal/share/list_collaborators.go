// share/list_collaborators.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"strings"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ListCollaborators returns explicit grants with real display identities,
// optionally filtered by q（按用户名/显示名/条目 id 匹配）。
func (service *Service) ListCollaborators(
	ctx context.Context,
	request *sharev1.ListCollaboratorsRequest,
) (*sharev1.ListCollaboratorsResponse, error) {
	if request == nil || request.GetBaseId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if _, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	); err != nil {
		return nil, err
	}
	collaborators, err := service.repository.ListCollaborators(ctx, request.GetBaseId())
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	userIDs := make([]string, 0, len(collaborators))
	for _, collaborator := range collaborators {
		userIDs = append(userIDs, collaborator.UserID)
	}
	profiles, err := service.profilesFor(ctx, userIDs)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	query := strings.ToLower(strings.TrimSpace(request.GetQ()))
	response := &sharev1.ListCollaboratorsResponse{
		CollaboratorList: make([]*sharev1.Collaborator, 0, len(collaborators)),
	}
	for _, collaborator := range collaborators {
		if query != "" && !collaboratorMatchesQuery(collaborator, profiles, query) {
			continue
		}
		response.CollaboratorList = append(
			response.CollaboratorList,
			collaboratorEntry(collaborator, profiles),
		)
	}
	return response, nil
}

// collaboratorMatchesQuery reports whether q hits the entry's user id, username
// or display name（q 过滤面向真实身份，而非仅一串 ID）。
func collaboratorMatchesQuery(
	collaborator base.ShareCollaborator,
	profiles map[string]*iamv1.User,
	query string,
) bool {
	if strings.Contains(strings.ToLower(collaborator.UserID), query) {
		return true
	}
	profile := profiles[collaborator.UserID]
	if profile == nil {
		return false
	}
	if strings.Contains(strings.ToLower(profile.GetUsername()), query) {
		return true
	}
	return strings.Contains(strings.ToLower(profile.GetDisplayName()), query)
}
