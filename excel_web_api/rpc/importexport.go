// rpc/importexport.go：worker importexport gRPC client 封装。
package rpc

import (
	"context"

	"google.golang.org/grpc"

	importexportv1 "excel/api/importexport/v1"
)

// ImportExportRPC is the import/export surface consumed by the HTTP gateway.
type ImportExportRPC interface {
	PreviewImport(
		context.Context,
		*importexportv1.PreviewImportRequest,
		...grpc.CallOption,
	) (*importexportv1.PreviewImportResponse, error)
	CreateImport(
		context.Context,
		*importexportv1.CreateImportRequest,
		...grpc.CallOption,
	) (*importexportv1.CreateImportResponse, error)
	GetImportStatus(
		context.Context,
		*importexportv1.GetImportStatusRequest,
		...grpc.CallOption,
	) (*importexportv1.GetImportStatusResponse, error)
	GetImportReport(
		context.Context,
		*importexportv1.GetImportReportRequest,
		...grpc.CallOption,
	) (importexportv1.ImportExportService_GetImportReportClient, error)
	CreateExport(
		context.Context,
		*importexportv1.CreateExportRequest,
		...grpc.CallOption,
	) (*importexportv1.CreateExportResponse, error)
	ExportStream(
		context.Context,
		*importexportv1.ExportStreamRequest,
		...grpc.CallOption,
	) (importexportv1.ImportExportService_ExportStreamClient, error)
}

// ImportExportClient adapts the generated worker import/export client.
type ImportExportClient struct {
	client importexportv1.ImportExportServiceClient
}

var _ ImportExportRPC = (*ImportExportClient)(nil)

// NewImportExportClient wraps a worker-service gRPC connection.
func NewImportExportClient(connection grpc.ClientConnInterface) *ImportExportClient {
	return &ImportExportClient{
		client: importexportv1.NewImportExportServiceClient(connection),
	}
}

// PreviewImport forwards one upload preview request.
func (client *ImportExportClient) PreviewImport(
	ctx context.Context,
	request *importexportv1.PreviewImportRequest,
	options ...grpc.CallOption,
) (*importexportv1.PreviewImportResponse, error) {
	return client.client.PreviewImport(ctx, request, options...)
}

// CreateImport starts one import task.
func (client *ImportExportClient) CreateImport(
	ctx context.Context,
	request *importexportv1.CreateImportRequest,
	options ...grpc.CallOption,
) (*importexportv1.CreateImportResponse, error) {
	return client.client.CreateImport(ctx, request, options...)
}

// GetImportStatus reads one import task.
func (client *ImportExportClient) GetImportStatus(
	ctx context.Context,
	request *importexportv1.GetImportStatusRequest,
	options ...grpc.CallOption,
) (*importexportv1.GetImportStatusResponse, error) {
	return client.client.GetImportStatus(ctx, request, options...)
}

// GetImportReport starts one report stream.
func (client *ImportExportClient) GetImportReport(
	ctx context.Context,
	request *importexportv1.GetImportReportRequest,
	options ...grpc.CallOption,
) (importexportv1.ImportExportService_GetImportReportClient, error) {
	return client.client.GetImportReport(ctx, request, options...)
}

// CreateExport starts one current-view export.
func (client *ImportExportClient) CreateExport(
	ctx context.Context,
	request *importexportv1.CreateExportRequest,
	options ...grpc.CallOption,
) (*importexportv1.CreateExportResponse, error) {
	return client.client.CreateExport(ctx, request, options...)
}

// ExportStream starts one export file stream.
func (client *ImportExportClient) ExportStream(
	ctx context.Context,
	request *importexportv1.ExportStreamRequest,
	options ...grpc.CallOption,
) (importexportv1.ImportExportService_ExportStreamClient, error) {
	return client.client.ExportStream(ctx, request, options...)
}
