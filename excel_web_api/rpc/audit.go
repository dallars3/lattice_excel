// rpc/audit.go：该模块 gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"

	auditv1 "excel/api/audit/v1"
)

// AuditRPC is the audit surface consumed by the HTTP gateway.
type AuditRPC interface {
	Query(context.Context, *auditv1.QueryRequest) (*auditv1.QueryResponse, error)
	ExportCSV(context.Context, *auditv1.ExportCSVRequest) (auditv1.AuditService_ExportCSVClient, error)
}

// AuditClient adapts the generated audit gRPC client to the gateway's AuditRPC seam.
type AuditClient struct {
	client auditv1.AuditServiceClient
}

var _ AuditRPC = (*AuditClient)(nil)

// NewAuditClient wraps a core-service gRPC connection.
func NewAuditClient(connection grpc.ClientConnInterface) *AuditClient {
	return &AuditClient{client: auditv1.NewAuditServiceClient(connection)}
}

// Query forwards a page of audit events.
func (client *AuditClient) Query(
	ctx context.Context,
	request *auditv1.QueryRequest,
) (*auditv1.QueryResponse, error) {
	return client.client.Query(ctx, request)
}

// ExportCSV starts a server-streamed audit CSV download.
func (client *AuditClient) ExportCSV(
	ctx context.Context,
	request *auditv1.ExportCSVRequest,
) (auditv1.AuditService_ExportCSVClient, error) {
	return client.client.ExportCSV(ctx, request)
}
