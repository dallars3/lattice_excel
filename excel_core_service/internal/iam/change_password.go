// iam/change_password.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
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

// ChangePassword verifies the old credential before replacing it with a bcrypt hash.
func (service *IamService) ChangePassword(
	ctx context.Context,
	request *iamv1.ChangePasswordRequest,
) (*emptypb.Empty, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil || request.GetOldPassword() == "" || !validPassword(request.GetNewPassword()) {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	user, err := service.repository.UserByID(ctx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.GetOldPassword())); err != nil {
		return nil, rpcError(deal.CodeLoginFailed)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.GetNewPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdatePasswordHash(ctx, user.ID, string(passwordHash)); errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	} else if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
