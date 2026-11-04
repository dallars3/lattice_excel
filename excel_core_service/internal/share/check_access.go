// share/check_access.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"

	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// CheckAccess is the single authorization decision point for Base actions.
func (service *Service) CheckAccess(
	ctx context.Context,
	request *sharev1.CheckAccessRequest,
) (*sharev1.CheckAccessResponse, error) {
	if request == nil || actionName(request.GetAction()) == "" {
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
	response := &sharev1.CheckAccessResponse{
		Role:     protoRole(decision.role),
		Internal: decision.internal,
		RowScope: rowScope(decision.role),
	}
	roles, err := service.policy.current()
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	response.Allow = service.decisionAllows(request.GetAction(), decision, roles)
	if service.importLock != nil {
		importing, lockErr := service.importLock.Importing(ctx, request.GetBaseId())
		if lockErr == nil && importing {
			// 导入中的 Base 全员不可访问（ticket 16）；锁查询失败不阻断裁决，
			// 其余查询在基础设施故障时会自然报错。
			response.Allow = false
		}
	}
	return response, nil
}
