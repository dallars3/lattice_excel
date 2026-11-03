// rpc/records.go：core.records 客户端。
package rpc

import (
	"context"

	"google.golang.org/grpc"

	recordsv1 "excel/api/records/v1"
)

// RecordsRPC is the core records surface used by import and export jobs.
type RecordsRPC interface {
	QueryView(
		context.Context,
		*recordsv1.QueryViewRequest,
	) (*recordsv1.QueryViewResponse, error)
	BatchUpdate(
		context.Context,
		*recordsv1.BatchUpdateRequest,
	) (*recordsv1.BatchUpdateResponse, error)
}

// RecordsClient adapts the generated core records client.
type RecordsClient struct {
	client recordsv1.RecordsServiceClient
}

var _ RecordsRPC = (*RecordsClient)(nil)

// NewRecordsClient wraps a core-service gRPC connection.
func NewRecordsClient(connection grpc.ClientConnInterface) *RecordsClient {
	return &RecordsClient{client: recordsv1.NewRecordsServiceClient(connection)}
}

// QueryView forwards one view page query.
func (client *RecordsClient) QueryView(
	ctx context.Context,
	request *recordsv1.QueryViewRequest,
) (*recordsv1.QueryViewResponse, error) {
	return client.client.QueryView(ctx, request)
}

// BatchUpdate forwards one batch import page.
func (client *RecordsClient) BatchUpdate(
	ctx context.Context,
	request *recordsv1.BatchUpdateRequest,
) (*recordsv1.BatchUpdateResponse, error) {
	return client.client.BatchUpdate(ctx, request)
}
