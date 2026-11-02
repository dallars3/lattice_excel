// calc/get_recalc_status.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package calc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	calcapi "excel/api/calc/v1"
)

// GetRecalcStatus reads the persisted materialization status for one cell.
func (service *Service) GetRecalcStatus(
	ctx context.Context,
	request *calcapi.GetRecalcStatusRequest,
) (*calcapi.GetRecalcStatusResponse, error) {
	if request == nil || request.GetRecordId() == "" || request.GetFieldId() == "" {
		return nil, status.Error(codes.InvalidArgument, "record_id and field_id are required")
	}
	if service == nil || service.repository == nil {
		return nil, status.Error(codes.FailedPrecondition, "calc repository is required")
	}
	statusValue, err := service.repository.Status(ctx, request.GetRecordId(), request.GetFieldId())
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "query recalc status: %v", err)
	}
	response := &calcapi.GetRecalcStatusResponse{}
	switch statusValue {
	case "IDLE", "":
		response.Status = calcapi.RecalcStatus_RECALC_STATUS_IDLE
	case "CALC":
		response.Status = calcapi.RecalcStatus_RECALC_STATUS_CALC
	case "ERROR":
		response.Status = calcapi.RecalcStatus_RECALC_STATUS_ERROR
	default:
		return nil, status.Errorf(codes.Internal, "unknown recalc status %q", statusValue)
	}
	return response, nil
}
