// share/check_access.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"
	"strings"

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
	if token, probe := anonymousLinkToken(request); probe {
		return service.checkAnonymousLink(ctx, token)
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
		if lockErr == nil && importing && request.GetAction() != sharev1.Action_ACTION_MANAGE_SHARE {
			// 导入中的 Base 数据面全员不可访问（ticket 16）；管理面（MANAGE_SHARE，
			// 如导入中补加协作者）放行：半成品数据仍不可读，成员管理属所有者治理
			// 操作（TC-140-08 隔离窗口内加协作者）。锁查询失败不阻断裁决，其余查询
			// 在基础设施故障时会自然报错。
			response.Allow = false
		}
	}
	return response, nil
}

// anonymousLinkToken identifies the /s/{token} entry probe from an
// unauthenticated visitor（H-95）：无 Principal 且显式携带 link_context 的
// CheckAccess 请求是链接有效性探测，仅凭 token 判定；带 Principal 的请求
// （含 link_context 缺省的无身份请求）沿用既有裁决路径，保持 1001 语义不变。
func anonymousLinkToken(request *sharev1.CheckAccessRequest) (string, bool) {
	if request.GetPrincipal().GetUserId() != "" {
		return "", false
	}
	if request.GetLinkContext() == nil {
		return "", false
	}
	return strings.TrimSpace(request.GetLinkContext().GetLinkToken()), true
}

// checkAnonymousLink judges whether the link token held by an unauthenticated
// visitor is currently live. 伪造/已重置/已关闭/导入中的 Base 一律 allow=false
// （业务否定，不是基础设施错误），网关据此统一 302 失效页。判定口径与 IAM
// 注册通道的 ResolveLinkToken 共用 validShareLinkRow，保证入口与注册对同一
// token 结论一致。
func (service *Service) checkAnonymousLink(
	ctx context.Context,
	token string,
) (*sharev1.CheckAccessResponse, error) {
	link, err := service.repository.LinkByToken(ctx, token)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return &sharev1.CheckAccessResponse{Allow: false}, nil
	case err != nil:
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if !validShareLinkRow(link) {
		return &sharev1.CheckAccessResponse{Allow: false}, nil
	}
	if service.importLock != nil {
		importing, lockErr := service.importLock.Importing(ctx, link.BaseID)
		if lockErr == nil && importing {
			return &sharev1.CheckAccessResponse{Allow: false}, nil
		}
	}
	role := normalizeRole(link.Role)
	return &sharev1.CheckAccessResponse{
		Allow:    true,
		Role:     protoRole(role),
		RowScope: rowScope(role),
	}, nil
}
