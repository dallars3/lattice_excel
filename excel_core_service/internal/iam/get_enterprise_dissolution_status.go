// iam/get_enterprise_dissolution_status.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// GetEnterpriseDissolutionStatus reports the owner-visible lifecycle state.
func (service *IamService) GetEnterpriseDissolutionStatus(
	ctx context.Context,
	_ *emptypb.Empty,
) (*iamv1.GetEnterpriseDissolutionStatusResponse, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	enterprise, err := service.repository.EnterpriseByID(ctx, principal.GetEnterpriseId())
	if err != nil || enterprise.OwnerID != principal.GetUserId() {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	status := enterprise.DissolutionStatus
	if status == "" {
		status = "none"
	}
	if status == "pending" &&
		enterprise.DissolutionDeadline > 0 &&
		service.now().UTC().UnixMilli() >= enterprise.DissolutionDeadline {
		status = "cooling"
	}
	return &iamv1.GetEnterpriseDissolutionStatusResponse{
		Status:     dissolutionStatusEnum(status),
		DeadlineMs: enterprise.DissolutionDeadline,
	}, nil
}
