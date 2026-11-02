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

// RecordsClient adapts the generated gRPC client to RecordsRPC.
type RecordsClient struct {
	client recordsv1.RecordsServiceClient
}

var _ RecordsRPC = (*RecordsClient)(nil)

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
