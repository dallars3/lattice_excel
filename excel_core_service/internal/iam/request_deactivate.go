// iam/request_deactivate.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

const userDeactivationCooldown = 7 * 24 * time.Hour

// RequestDeactivate starts a reversible seven-day account cancellation window.
func (service *IamService) RequestDeactivate(
	ctx context.Context,
	_ *emptypb.Empty,
) (*iamv1.RequestDeactivateResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	user, err := service.repository.UserByIDForUpdate(ctx, tx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if user.Status == "deactivated" {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if user.Status == "pending" && user.DeactivateAt > 0 {
		return &iamv1.RequestDeactivateResponse{DeadlineMs: user.DeactivateAt}, nil
	}
	deadline := service.now().UTC().Add(userDeactivationCooldown).UnixMilli()
	if err := service.repository.RequestUserDeactivationTx(ctx, tx, user.ID, deadline); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &iamv1.RequestDeactivateResponse{DeadlineMs: deadline}, nil
}
