// iam/logout.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// Logout invalidates the supplied session token without affecting other sessions.
func (service *IamService) Logout(ctx context.Context, request *iamv1.LogoutRequest) (*emptypb.Empty, error) {
	if request == nil || request.GetToken() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if err := service.sessions.Delete(ctx, request.GetToken()); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
