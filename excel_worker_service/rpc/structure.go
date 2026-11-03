// rpc/structure.go：core.structure 客户端。
package rpc

import (
	"context"

	"google.golang.org/grpc"

	structurev1 "excel/api/structure/v1"
)

// StructureRPC is the core structure surface used by import/export jobs.
type StructureRPC interface {
	ListTables(
		context.Context,
		*structurev1.ListTablesRequest,
	) (*structurev1.ListTablesResponse, error)
	CreateTable(
		context.Context,
		*structurev1.CreateTableRequest,
	) (*structurev1.CreateTableResponse, error)
	ListFields(
		context.Context,
		*structurev1.ListFieldsRequest,
	) (*structurev1.ListFieldsResponse, error)
	CreateField(
		context.Context,
		*structurev1.CreateFieldRequest,
	) (*structurev1.CreateFieldResponse, error)
	ListViews(
		context.Context,
		*structurev1.ListViewsRequest,
	) (*structurev1.ListViewsResponse, error)
	GetViewConfig(
		context.Context,
		*structurev1.GetViewConfigRequest,
	) (*structurev1.GetViewConfigResponse, error)
}

// StructureClient adapts the generated core structure client.
type StructureClient struct {
	client structurev1.StructureServiceClient
}

var _ StructureRPC = (*StructureClient)(nil)

// NewStructureClient wraps a core-service gRPC connection.
func NewStructureClient(connection grpc.ClientConnInterface) *StructureClient {
	return &StructureClient{client: structurev1.NewStructureServiceClient(connection)}
}

// ListTables forwards one table list lookup.
func (client *StructureClient) ListTables(
	ctx context.Context,
	request *structurev1.ListTablesRequest,
) (*structurev1.ListTablesResponse, error) {
	return client.client.ListTables(ctx, request)
}

// CreateTable forwards one table creation.
func (client *StructureClient) CreateTable(
	ctx context.Context,
	request *structurev1.CreateTableRequest,
) (*structurev1.CreateTableResponse, error) {
	return client.client.CreateTable(ctx, request)
}

// ListFields forwards one field list lookup.
func (client *StructureClient) ListFields(
	ctx context.Context,
	request *structurev1.ListFieldsRequest,
) (*structurev1.ListFieldsResponse, error) {
	return client.client.ListFields(ctx, request)
}

// CreateField forwards one field creation.
func (client *StructureClient) CreateField(
	ctx context.Context,
	request *structurev1.CreateFieldRequest,
) (*structurev1.CreateFieldResponse, error) {
	return client.client.CreateField(ctx, request)
}

// ListViews forwards one view list lookup.
func (client *StructureClient) ListViews(
	ctx context.Context,
	request *structurev1.ListViewsRequest,
) (*structurev1.ListViewsResponse, error) {
	return client.client.ListViews(ctx, request)
}

// GetViewConfig forwards one view configuration lookup.
func (client *StructureClient) GetViewConfig(
	ctx context.Context,
	request *structurev1.GetViewConfigRequest,
) (*structurev1.GetViewConfigResponse, error) {
	return client.client.GetViewConfig(ctx, request)
}
