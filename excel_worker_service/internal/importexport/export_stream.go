// importexport/export_stream.go：已完成导出文件的服务端流。
package importexport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc/metadata"

	importexportv1 "excel/api/importexport/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// ExportStream streams a DONE export owned by the caller.
func (service *Service) ExportStream(
	request *importexportv1.ExportStreamRequest,
	stream importexportv1.ImportExportService_ExportStreamServer,
) error {
	if service == nil || service.tasks == nil || service.files == nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetTaskId() == "" {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(stream.Context())
	if err != nil {
		return err
	}
	task, err := service.tasks.Get(stream.Context(), request.GetTaskId())
	if errors.Is(err, base.ErrImportExportTaskNotFound) ||
		task.Kind != "export" ||
		task.Status != statusDone ||
		task.FileID == "" {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	var payload exportPayload
	if len(task.Payload) > 0 {
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return fmt.Errorf("decode export task payload: %w", err)
		}
	}
	if payload.CreatorID != "" && payload.CreatorID != principal.GetUserId() {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	meta, err := service.files.Get(stream.Context(), task.FileID)
	if errors.Is(err, base.ErrFileNotFound) {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(service.attachDir, filepath.FromSlash(meta.StoragePath)))
	if err != nil {
		if os.IsNotExist(err) {
			return deal.RPCError(deal.CodeDeletedResource)
		}
		return err
	}
	defer file.Close()
	_ = stream.SetHeader(metadata.Pairs(
		"x-file-name", meta.DisplayName,
		"x-file-mime", meta.Mime,
	))
	return streamExportChunks(file, stream)
}
