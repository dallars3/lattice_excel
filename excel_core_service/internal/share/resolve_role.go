// share/resolve_role.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"

	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// ResolveRole returns the effective role used by row-level consumers.
func (service *Service) ResolveRole(
	ctx context.Context,
	request *sharev1.ResolveRoleRequest,
) (*sharev1.ResolveRoleResponse, error) {
	if request == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	decision, err := service.resolveDecision(
		ctx,
		request.GetPrincipal(),
		request.GetBaseId(),
		request.GetLinkContext(),
	)
	if err != nil {
		return nil, err
	}
	return &sharev1.ResolveRoleResponse{
		Role:     protoRole(decision.role),
		Internal: decision.internal,
		RowScope: rowScope(decision.role),
	}, nil
}
