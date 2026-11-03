// importexport/get_import_status.go：发起人可见的导入任务状态。
package importexport

import (
	"context"
	"encoding/json"
	"errors"

	importexportv1 "excel/api/importexport/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// GetImportStatus returns one import task visible to its creator.
func (service *Service) GetImportStatus(
	ctx context.Context,
	request *importexportv1.GetImportStatusRequest,
) (*importexportv1.GetImportStatusResponse, error) {
	if service == nil || service.tasks == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetTaskId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	task, err := service.tasks.Get(ctx, request.GetTaskId())
	if errors.Is(err, base.ErrImportExportTaskNotFound) || task.Kind != "import" {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return nil, err
	}
	var payload struct {
		CreatorID string `json:"creator_id"`
	}
	if len(task.Payload) > 0 {
		_ = json.Unmarshal(task.Payload, &payload)
	}
	if payload.CreatorID != "" && payload.CreatorID != principal.GetUserId() {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	protoTask, err := taskToProto(task)
	if err != nil {
		return nil, err
	}
	return &importexportv1.GetImportStatusResponse{ImportTask: protoTask}, nil
}
