// importexport/export_stream.go：已完成导出文件的服务端流。
package importexport

import (
	"encoding/json"
	"errors"
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
		return rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetTaskId() == "" {
		return rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(stream.Context())
	if err != nil {
		return err
	}
	task, err := service.tasks.Get(stream.Context(), request.GetTaskId())
	if errors.Is(err, base.ErrImportExportTaskNotFound) ||
		task.Kind != "export" ||
		task.Status != statusDone ||
		task.FileID == "" {
		return rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	var payload exportPayload
	if len(task.Payload) > 0 {
		_ = json.Unmarshal(task.Payload, &payload)
	}
	if payload.CreatorID != "" && payload.CreatorID != principal.GetUserId() {
		return rpcError(deal.CodePermissionDenied)
	}
	meta, err := service.files.Get(stream.Context(), task.FileID)
	if errors.Is(err, base.ErrFileNotFound) {
		return rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(service.attachDir, filepath.FromSlash(meta.StoragePath)))
	if err != nil {
		if os.IsNotExist(err) {
			return rpcError(deal.CodeDeletedResource)
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
