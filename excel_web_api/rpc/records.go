// rpc/records.go：该模块 gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"

	recordsv1 "excel/api/records/v1"
)

// RecordsRPC is the records read subset consumed by the HTTP gateway.
type RecordsRPC interface {
	QueryView(
		context.Context,
		*recordsv1.QueryViewRequest,
	) (*recordsv1.QueryViewResponse, error)
	GetRecord(
		context.Context,
		*recordsv1.GetRecordRequest,
	) (*recordsv1.GetRecordResponse, error)
	GetHistory(
		context.Context,
		*recordsv1.GetHistoryRequest,
	) (*recordsv1.GetHistoryResponse, error)
}

// RecordsWriteRPC extends the read subset with the records mutation surface.
type RecordsWriteRPC interface {
	RecordsRPC
	CreateRecord(
		context.Context,
		*recordsv1.CreateRecordRequest,
	) (*recordsv1.CreateRecordResponse, error)
	UpdateCell(
		context.Context,
		*recordsv1.UpdateCellRequest,
	) (*recordsv1.UpdateCellResponse, error)
	BatchUpdate(
		context.Context,
		*recordsv1.BatchUpdateRequest,
	) (*recordsv1.BatchUpdateResponse, error)
	DeleteRecords(
		context.Context,
		*recordsv1.DeleteRecordsRequest,
	) (*recordsv1.DeleteRecordsResponse, error)
	UndoDelete(
		context.Context,
		*recordsv1.UndoDeleteRequest,
	) (*recordsv1.UndoDeleteResponse, error)
	Rollback(
		context.Context,
		*recordsv1.RollbackRequest,
	) (*recordsv1.RollbackResponse, error)
}

// RecordsClient adapts the generated gRPC client to RecordsRPC.
type RecordsClient struct {
	client recordsv1.RecordsServiceClient
}

var _ RecordsRPC = (*RecordsClient)(nil)
var _ RecordsWriteRPC = (*RecordsClient)(nil)

// NewRecordsClient wraps a core-service gRPC connection.
func NewRecordsClient(connection grpc.ClientConnInterface) *RecordsClient {
	return &RecordsClient{client: recordsv1.NewRecordsServiceClient(connection)}
}

// QueryView forwards one view query.
func (client *RecordsClient) QueryView(
	ctx context.Context,
	request *recordsv1.QueryViewRequest,
) (*recordsv1.QueryViewResponse, error) {
	return client.client.QueryView(ctx, request)
}

// GetRecord forwards one record lookup.
func (client *RecordsClient) GetRecord(
	ctx context.Context,
	request *recordsv1.GetRecordRequest,
) (*recordsv1.GetRecordResponse, error) {
	return client.client.GetRecord(ctx, request)
}

// GetHistory forwards one history page lookup.
func (client *RecordsClient) GetHistory(
	ctx context.Context,
	request *recordsv1.GetHistoryRequest,
) (*recordsv1.GetHistoryResponse, error) {
	return client.client.GetHistory(ctx, request)
}

// CreateRecord forwards one record creation.
func (client *RecordsClient) CreateRecord(
	ctx context.Context,
	request *recordsv1.CreateRecordRequest,
) (*recordsv1.CreateRecordResponse, error) {
	return client.client.CreateRecord(ctx, request)
}

// UpdateCell forwards one optimistic-locked cell update.
func (client *RecordsClient) UpdateCell(
	ctx context.Context,
	request *recordsv1.UpdateCellRequest,
) (*recordsv1.UpdateCellResponse, error) {
	return client.client.UpdateCell(ctx, request)
}

// BatchUpdate forwards one batch or paste operation.
func (client *RecordsClient) BatchUpdate(
	ctx context.Context,
	request *recordsv1.BatchUpdateRequest,
) (*recordsv1.BatchUpdateResponse, error) {
	return client.client.BatchUpdate(ctx, request)
}

// DeleteRecords forwards one soft-delete request.
func (client *RecordsClient) DeleteRecords(
	ctx context.Context,
	request *recordsv1.DeleteRecordsRequest,
) (*recordsv1.DeleteRecordsResponse, error) {
	return client.client.DeleteRecords(ctx, request)
}

// UndoDelete forwards one seven-second undo request.
func (client *RecordsClient) UndoDelete(
	ctx context.Context,
	request *recordsv1.UndoDeleteRequest,
) (*recordsv1.UndoDeleteResponse, error) {
	return client.client.UndoDelete(ctx, request)
}

// Rollback forwards one history rollback request.
func (client *RecordsClient) Rollback(
	ctx context.Context,
	request *recordsv1.RollbackRequest,
) (*recordsv1.RollbackResponse, error) {
	return client.client.Rollback(ctx, request)
}
