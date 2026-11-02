// workspace/search_bases.go：一文件一接口。
package workspace

import (
	"context"
	"strings"

	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/deal"
)

// SearchBases searches only Base names within the caller's accessible scope.
func (service *Service) SearchBases(
	ctx context.Context,
	request *workspacev1.SearchBasesRequest,
) (*workspacev1.SearchBasesResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if service.repository == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	items, err := service.repository.SearchBases(
		ctx,
		principal.GetUserId(),
		strings.TrimSpace(request.GetQ()),
	)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	response := &workspacev1.SearchBasesResponse{
		BaseList: make([]*workspacev1.Base, 0, len(items)),
	}
	for _, item := range items {
		response.BaseList = append(response.BaseList, workspaceBaseToProto(item))
	}
	return response, nil
}
