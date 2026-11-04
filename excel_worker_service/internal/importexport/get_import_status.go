// importexport/get_import_status.go：发起人可见的导入任务状态。
package importexport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetTaskId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	task, err := service.tasks.Get(ctx, request.GetTaskId())
	if errors.Is(err, base.ErrImportExportTaskNotFound) || task.Kind != "import" {
		return nil, deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return nil, err
	}
	var payload struct {
		CreatorID string `json:"creator_id"`
	}
	if len(task.Payload) > 0 {
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode import task payload: %w", err)
		}
	}
	if payload.CreatorID != "" && payload.CreatorID != principal.GetUserId() {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	protoTask, err := taskToProto(task)
	if err != nil {
		return nil, err
	}
	return &importexportv1.GetImportStatusResponse{ImportTask: protoTask}, nil
}
