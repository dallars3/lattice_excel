// iam/cancel_deactivate.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	"excel_core_service/deal"
)

// CancelDeactivate reverses a pending account cancellation.
func (service *IamService) CancelDeactivate(
	ctx context.Context,
	_ *emptypb.Empty,
) (*emptypb.Empty, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	user, err := service.repository.UserByIDForUpdate(ctx, tx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if user.Status != "pending" ||
		user.DeactivateAt <= service.now().UTC().UnixMilli() {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err := service.repository.CancelUserDeactivationTx(ctx, tx, user.ID); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
