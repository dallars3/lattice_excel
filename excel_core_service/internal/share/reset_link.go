// share/reset_link.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"

	"excel-libs/idgen"
	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// ResetLink rotates the URL token and invalidates every session holding the old token.
func (service *Service) ResetLink(
	ctx context.Context,
	request *sharev1.ResetLinkRequest,
) (*sharev1.ResetLinkResponse, error) {
	if request == nil || request.GetBaseId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	)
	if err != nil {
		return nil, err
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, request.GetBaseId())
	if err != nil || enterpriseID == "" {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	link, err := service.repository.LinkByBaseIDForUpdate(ctx, tx, request.GetBaseId())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		link, err = service.newLink(request.GetBaseId())
	case err != nil:
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	token, err := idgen.New()
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	link.Token = token
	link.URL = "/s/" + token
	link.UpdatedAt = service.now().UTC().UnixMilli()
	if err := service.repository.UpsertLink(ctx, tx, link); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueLinkClosed(ctx, tx, linkClosedPayload{
		BaseID:       link.BaseID,
		LinkID:       link.BaseID,
		EnterpriseID: enterpriseID,
		Operator:     principal.GetUserId(),
		Reason:       "reset",
	}); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &sharev1.ResetLinkResponse{LinkConfig: protoLinkConfig(link)}, nil
}
