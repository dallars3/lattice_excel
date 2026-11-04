// iam/set_register_switch.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// SetRegisterSwitch changes the global registration gate.
func (service *IamService) SetRegisterSwitch(
	ctx context.Context,
	request *iamv1.SetRegisterSwitchRequest,
) (*emptypb.Empty, error) {
	if err := service.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	now := service.now().UTC()
	if err := service.repository.SetRegisterSwitchTx(ctx, tx, request.GetEnabled(), now.UnixMilli()); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueAudit(
		ctx,
		tx,
		"AUDIT_ACTION_REGISTER_SWITCH_CHANGED",
		"register_switch",
		map[string]any{"enabled": request.GetEnabled()},
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
