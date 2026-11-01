// iam/update_profile.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// UpdateProfile changes the authenticated user's display name and avatar.
func (service *IamService) UpdateProfile(
	ctx context.Context,
	request *iamv1.UpdateProfileRequest,
) (*iamv1.UpdateProfileResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	displayName := strings.TrimSpace(request.GetDisplayName())
	if count := utf8.RuneCountInString(displayName); count < 1 || count > 20 || len(request.GetAvatarUri()) > 512 {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if err := service.repository.UpdateUserProfile(
		ctx,
		principal.GetUserId(),
		displayName,
		request.GetAvatarUri(),
	); errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	} else if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	user, err := service.repository.UserByID(ctx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &iamv1.UpdateProfileResponse{User: protoUser(user)}, nil
}
