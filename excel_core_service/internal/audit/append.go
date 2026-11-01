// audit/append.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package audit

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	auditv1 "excel/api/audit/v1"
)

// Append persists an action-level audit event using event_id as its idempotency key.
func (service *Service) Append(
	ctx context.Context,
	request *auditv1.AppendRequest,
) (*emptypb.Empty, error) {
	if err := service.appendRequest(ctx, request); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
