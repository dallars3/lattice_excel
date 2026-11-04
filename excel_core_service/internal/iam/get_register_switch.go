// iam/get_register_switch.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// GetRegisterSwitch returns the authoritative registration switch state.
func (service *IamService) GetRegisterSwitch(
	ctx context.Context,
	_ *emptypb.Empty,
) (*iamv1.GetRegisterSwitchResponse, error) {
	if err := service.requireAdmin(ctx); err != nil {
		return nil, err
	}
	enabled, err := service.repository.RegisterSwitchEnabled(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &iamv1.GetRegisterSwitchResponse{Enabled: enabled}, nil
}
