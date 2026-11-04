// iam/admin_reset_password.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// AdminResetPassword replaces an account credential and records the operator action.
func (service *IamService) AdminResetPassword(
	ctx context.Context,
	request *iamv1.AdminResetPasswordRequest,
) (*emptypb.Empty, error) {
	if err := service.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if request == nil ||
		!validUsername(request.GetUsername()) ||
		!validPassword(request.GetNewPassword()) {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(request.GetNewPassword()),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	user, err := service.repository.UserByUsernameForUpdate(ctx, tx, request.GetUsername())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdatePasswordHashTx(
		ctx,
		tx,
		user.ID,
		string(passwordHash),
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueAudit(
		ctx,
		tx,
		"AUDIT_ACTION_ADMIN_RESET_PASSWORD",
		user.Username,
		map[string]any{"user_id": user.ID},
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
