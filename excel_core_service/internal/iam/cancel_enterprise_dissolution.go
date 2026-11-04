// iam/cancel_enterprise_dissolution.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"excel_core_service/deal"
)

// CancelEnterpriseDissolution revokes a pending owner request during cooldown.
func (service *IamService) CancelEnterpriseDissolution(
	ctx context.Context,
	_ *emptypb.Empty,
) (*emptypb.Empty, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	enterprise, err := service.repository.EnterpriseByIDForUpdate(
		ctx,
		tx,
		principal.GetEnterpriseId(),
	)
	if err != nil || enterprise.OwnerID != principal.GetUserId() {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if enterprise.DissolutionStatus != "pending" ||
		enterprise.DissolutionDeadline <= service.now().UTC().UnixMilli() {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err := service.repository.SetEnterpriseDissolutionTx(
		ctx,
		tx,
		enterprise.ID,
		"revoked",
		enterprise.DissolutionDeadline,
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
