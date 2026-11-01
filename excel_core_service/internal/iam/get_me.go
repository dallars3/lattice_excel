// iam/get_me.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// GetMe returns the authenticated user's account, enterprise, and membership role.
func (service *IamService) GetMe(ctx context.Context, _ *emptypb.Empty) (*iamv1.GetMeResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	user, err := service.repository.UserByID(ctx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}

	response := &iamv1.GetMeResponse{User: protoUser(user)}
	if user.EnterpriseID == "" {
		return response, nil
	}
	enterprise, role, err := service.repository.EnterpriseAndRoleByUserID(ctx, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	response.Enterprise = &iamv1.Enterprise{
		Id:          enterprise.ID,
		Name:        enterprise.Name,
		OwnerId:     enterprise.OwnerID,
		MemberCount: enterprise.MemberCount,
		CreatedAt:   enterprise.CreatedAt,
	}
	response.MyRole = role
	return response, nil
}
