// importexport/get_import_report.go：导入错误报告的服务端流。
package importexport

import (
	"errors"
	"os"
	"path/filepath"

	"google.golang.org/grpc/metadata"

	importexportv1 "excel/api/importexport/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// GetImportReport streams the row-level CSV error report to its creator.
func (service *Service) GetImportReport(
	request *importexportv1.GetImportReportRequest,
	stream importexportv1.ImportExportService_GetImportReportServer,
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
		task.Kind != "import" ||
		task.ReportFileID == "" {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	report, err := service.files.Get(stream.Context(), task.ReportFileID)
	if errors.Is(err, base.ErrFileNotFound) {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	if report.Uploader != principal.GetUserId() {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	file, err := os.Open(filepath.Join(service.attachDir, filepath.FromSlash(report.StoragePath)))
	if err != nil {
		if os.IsNotExist(err) {
			return deal.RPCError(deal.CodeDeletedResource)
		}
		return err
	}
	defer file.Close()
	_ = stream.SetHeader(metadata.Pairs(
		"x-file-name", report.DisplayName,
		"x-file-mime", report.Mime,
	))
	return streamImportChunks(file, stream)
}
