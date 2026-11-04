// rpc/structure.go：structure gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	structurev1 "excel/api/structure/v1"
)

// StructureRPC is the structure surface consumed by the HTTP gateway.
type StructureRPC interface {
	ListTables(context.Context, *structurev1.ListTablesRequest) (*structurev1.ListTablesResponse, error)
	CreateTable(context.Context, *structurev1.CreateTableRequest) (*structurev1.CreateTableResponse, error)
	RenameTable(context.Context, *structurev1.RenameTableRequest) (*structurev1.RenameTableResponse, error)
	DuplicateTable(context.Context, *structurev1.DuplicateTableRequest) (*structurev1.DuplicateTableResponse, error)
	DeleteTable(context.Context, *structurev1.DeleteTableRequest) (*emptypb.Empty, error)
	ListFields(context.Context, *structurev1.ListFieldsRequest) (*structurev1.ListFieldsResponse, error)
	CreateField(context.Context, *structurev1.CreateFieldRequest) (*structurev1.CreateFieldResponse, error)
	RenameField(context.Context, *structurev1.RenameFieldRequest) (*structurev1.RenameFieldResponse, error)
	UpdateFieldConfig(context.Context, *structurev1.UpdateFieldConfigRequest) (*structurev1.UpdateFieldConfigResponse, error)
	ConvertField(context.Context, *structurev1.ConvertFieldRequest) (*structurev1.ConvertFieldResponse, error)
	DeleteField(context.Context, *structurev1.DeleteFieldRequest) (*structurev1.DeleteFieldResponse, error)
	UpdateOptions(context.Context, *structurev1.UpdateOptionsRequest) (*structurev1.UpdateOptionsResponse, error)
	DuplicateField(context.Context, *structurev1.DuplicateFieldRequest) (*structurev1.DuplicateFieldResponse, error)
	ListViews(context.Context, *structurev1.ListViewsRequest) (*structurev1.ListViewsResponse, error)
	CreateView(context.Context, *structurev1.CreateViewRequest) (*structurev1.CreateViewResponse, error)
	RenameView(context.Context, *structurev1.RenameViewRequest) (*structurev1.RenameViewResponse, error)
	DuplicateView(context.Context, *structurev1.DuplicateViewRequest) (*structurev1.DuplicateViewResponse, error)
	DeleteView(context.Context, *structurev1.DeleteViewRequest) (*emptypb.Empty, error)
	GetViewConfig(context.Context, *structurev1.GetViewConfigRequest) (*structurev1.GetViewConfigResponse, error)
	UpdateViewConfig(context.Context, *structurev1.UpdateViewConfigRequest) (*structurev1.UpdateViewConfigResponse, error)
}

// StructureClient adapts the generated structure gRPC client to StructureRPC.
type StructureClient struct {
	client structurev1.StructureServiceClient
}

var _ StructureRPC = (*StructureClient)(nil)

// NewStructureClient wraps a core-service gRPC connection.
func NewStructureClient(connection grpc.ClientConnInterface) *StructureClient {
	return &StructureClient{client: structurev1.NewStructureServiceClient(connection)}
}

// ListTables forwards a table list lookup.
func (client *StructureClient) ListTables(
	ctx context.Context,
	request *structurev1.ListTablesRequest,
) (*structurev1.ListTablesResponse, error) {
	return client.client.ListTables(ctx, request)
}

// CreateTable forwards a table creation.
func (client *StructureClient) CreateTable(
	ctx context.Context,
	request *structurev1.CreateTableRequest,
) (*structurev1.CreateTableResponse, error) {
	return client.client.CreateTable(ctx, request)
}

// RenameTable forwards a table rename.
func (client *StructureClient) RenameTable(
	ctx context.Context,
	request *structurev1.RenameTableRequest,
) (*structurev1.RenameTableResponse, error) {
	return client.client.RenameTable(ctx, request)
}

// DuplicateTable forwards a table duplication.
func (client *StructureClient) DuplicateTable(
	ctx context.Context,
	request *structurev1.DuplicateTableRequest,
) (*structurev1.DuplicateTableResponse, error) {
	return client.client.DuplicateTable(ctx, request)
}

// DeleteTable forwards a table deletion.
func (client *StructureClient) DeleteTable(
	ctx context.Context,
	request *structurev1.DeleteTableRequest,
) (*emptypb.Empty, error) {
	return client.client.DeleteTable(ctx, request)
}

// ListFields forwards a field list lookup.
func (client *StructureClient) ListFields(
	ctx context.Context,
	request *structurev1.ListFieldsRequest,
) (*structurev1.ListFieldsResponse, error) {
	return client.client.ListFields(ctx, request)
}

// CreateField forwards a field creation.
func (client *StructureClient) CreateField(
	ctx context.Context,
	request *structurev1.CreateFieldRequest,
) (*structurev1.CreateFieldResponse, error) {
	return client.client.CreateField(ctx, request)
}

// RenameField forwards a field rename.
func (client *StructureClient) RenameField(
	ctx context.Context,
	request *structurev1.RenameFieldRequest,
) (*structurev1.RenameFieldResponse, error) {
	return client.client.RenameField(ctx, request)
}

// UpdateFieldConfig forwards a field config update.
func (client *StructureClient) UpdateFieldConfig(
	ctx context.Context,
	request *structurev1.UpdateFieldConfigRequest,
) (*structurev1.UpdateFieldConfigResponse, error) {
	return client.client.UpdateFieldConfig(ctx, request)
}

// ConvertField forwards a field type conversion.
func (client *StructureClient) ConvertField(
	ctx context.Context,
	request *structurev1.ConvertFieldRequest,
) (*structurev1.ConvertFieldResponse, error) {
	return client.client.ConvertField(ctx, request)
}

// DeleteField forwards a field deletion.
func (client *StructureClient) DeleteField(
	ctx context.Context,
	request *structurev1.DeleteFieldRequest,
) (*structurev1.DeleteFieldResponse, error) {
	return client.client.DeleteField(ctx, request)
}

// UpdateOptions forwards a single/multi-select option list update.
func (client *StructureClient) UpdateOptions(
	ctx context.Context,
	request *structurev1.UpdateOptionsRequest,
) (*structurev1.UpdateOptionsResponse, error) {
	return client.client.UpdateOptions(ctx, request)
}

// DuplicateField forwards a field duplication.
func (client *StructureClient) DuplicateField(
	ctx context.Context,
	request *structurev1.DuplicateFieldRequest,
) (*structurev1.DuplicateFieldResponse, error) {
	return client.client.DuplicateField(ctx, request)
}

// ListViews forwards a view list lookup.
func (client *StructureClient) ListViews(
	ctx context.Context,
	request *structurev1.ListViewsRequest,
) (*structurev1.ListViewsResponse, error) {
	return client.client.ListViews(ctx, request)
}

// CreateView forwards a view creation.
func (client *StructureClient) CreateView(
	ctx context.Context,
	request *structurev1.CreateViewRequest,
) (*structurev1.CreateViewResponse, error) {
	return client.client.CreateView(ctx, request)
}

// RenameView forwards a view rename.
func (client *StructureClient) RenameView(
	ctx context.Context,
	request *structurev1.RenameViewRequest,
) (*structurev1.RenameViewResponse, error) {
	return client.client.RenameView(ctx, request)
}

// DuplicateView forwards a view duplication.
func (client *StructureClient) DuplicateView(
	ctx context.Context,
	request *structurev1.DuplicateViewRequest,
) (*structurev1.DuplicateViewResponse, error) {
	return client.client.DuplicateView(ctx, request)
}

// DeleteView forwards a view deletion.
func (client *StructureClient) DeleteView(
	ctx context.Context,
	request *structurev1.DeleteViewRequest,
) (*emptypb.Empty, error) {
	return client.client.DeleteView(ctx, request)
}

// GetViewConfig forwards a view config lookup.
func (client *StructureClient) GetViewConfig(
	ctx context.Context,
	request *structurev1.GetViewConfigRequest,
) (*structurev1.GetViewConfigResponse, error) {
	return client.client.GetViewConfig(ctx, request)
}

// UpdateViewConfig forwards a view config update.
func (client *StructureClient) UpdateViewConfig(
	ctx context.Context,
	request *structurev1.UpdateViewConfigRequest,
) (*structurev1.UpdateViewConfigResponse, error) {
	return client.client.UpdateViewConfig(ctx, request)
}
