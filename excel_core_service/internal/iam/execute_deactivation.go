// iam/execute_deactivation.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// ExecuteDeactivation permanently revokes credentials after the seven-day window.
func (service *IamService) ExecuteDeactivation(
	ctx context.Context,
	request *iamv1.ExecuteDeactivationRequest,
) (*emptypb.Empty, error) {
	if request == nil || strings.TrimSpace(request.GetUserId()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	user, err := service.repository.UserByIDForUpdate(ctx, tx, request.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if user.Status == "deactivated" || user.Status == "active" {
		return &emptypb.Empty{}, nil
	}
	if user.Status != "pending" ||
		user.DeactivateAt <= 0 ||
		service.now().UTC().UnixMilli() < user.DeactivateAt {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	ownerID := ""
	if user.EnterpriseID != "" {
		enterprise, lookupErr := service.repository.EnterpriseByID(ctx, user.EnterpriseID)
		if lookupErr == nil {
			ownerID = enterprise.OwnerID
		} else if !errors.Is(lookupErr, sql.ErrNoRows) {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
	}
	if service.bases != nil && ownerID != "" && ownerID != user.ID {
		if err := service.bases.ReassignAllBases(ctx, user.ID, ownerID); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
	}
	if err := service.repository.ExecuteUserDeactivationTx(ctx, tx, user.ID); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
