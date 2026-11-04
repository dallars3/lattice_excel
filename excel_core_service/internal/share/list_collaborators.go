// share/list_collaborators.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"strings"

	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// ListCollaborators returns explicit grants, optionally filtered by q.
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
	query := strings.ToLower(strings.TrimSpace(request.GetQ()))
	response := &sharev1.ListCollaboratorsResponse{
		CollaboratorList: make([]*sharev1.Collaborator, 0, len(collaborators)),
	}
	for _, collaborator := range collaborators {
		if query != "" && !strings.Contains(strings.ToLower(collaborator.UserID), query) {
			continue
		}
		response.CollaboratorList = append(response.CollaboratorList, protoCollaborator(collaborator))
	}
	return response, nil
}
