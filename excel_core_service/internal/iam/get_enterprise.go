// iam/get_enterprise.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// GetEnterprise returns the active member's enterprise and member count.
func (service *IamService) GetEnterprise(
	ctx context.Context,
	_ *emptypb.Empty,
) (*iamv1.GetEnterpriseResponse, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := service.activeMember(ctx, principal); err != nil {
		return nil, err
	}
	enterprise, err := service.repository.EnterpriseByID(ctx, principal.GetEnterpriseId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &iamv1.GetEnterpriseResponse{Enterprise: &iamv1.Enterprise{
		Id:          enterprise.ID,
		Name:        enterprise.Name,
		OwnerId:     enterprise.OwnerID,
		MemberCount: enterprise.MemberCount,
		CreatedAt:   enterprise.CreatedAt,
	}}, nil
}
