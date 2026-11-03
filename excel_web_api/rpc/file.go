// rpc/file.go：worker file gRPC client 封装。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	filev1 "excel/api/file/v1"
)

// FileRPC is the file streaming surface consumed by the HTTP gateway.
type FileRPC interface {
	InitUpload(
		context.Context,
		...grpc.CallOption,
	) (filev1.FileService_InitUploadClient, error)
	ConfirmUpload(
		context.Context,
		*filev1.ConfirmUploadRequest,
		...grpc.CallOption,
	) (*filev1.ConfirmUploadResponse, error)
	DownloadStream(
		context.Context,
		*filev1.DownloadStreamRequest,
		...grpc.CallOption,
	) (filev1.FileService_DownloadStreamClient, error)
	GetThumb(
		context.Context,
		*filev1.GetThumbRequest,
		...grpc.CallOption,
	) (filev1.FileService_GetThumbClient, error)
	RenameFile(
		context.Context,
		*filev1.RenameFileRequest,
		...grpc.CallOption,
	) (*filev1.RenameFileResponse, error)
	DeleteFile(
		context.Context,
		*filev1.DeleteFileRequest,
		...grpc.CallOption,
	) (*emptypb.Empty, error)
}

// FileClient adapts the generated worker file client.
type FileClient struct {
	client filev1.FileServiceClient
}

var _ FileRPC = (*FileClient)(nil)

// NewFileClient wraps a worker-service gRPC connection.
func NewFileClient(connection grpc.ClientConnInterface) *FileClient {
	return &FileClient{client: filev1.NewFileServiceClient(connection)}
}

// InitUpload starts one client-stream upload.
func (client *FileClient) InitUpload(
	ctx context.Context,
	options ...grpc.CallOption,
) (filev1.FileService_InitUploadClient, error) {
	return client.client.InitUpload(ctx, options...)
}

// ConfirmUpload finalizes one uploaded file.
func (client *FileClient) ConfirmUpload(
	ctx context.Context,
	request *filev1.ConfirmUploadRequest,
	options ...grpc.CallOption,
) (*filev1.ConfirmUploadResponse, error) {
	return client.client.ConfirmUpload(ctx, request, options...)
}

// DownloadStream starts one attachment stream.
func (client *FileClient) DownloadStream(
	ctx context.Context,
	request *filev1.DownloadStreamRequest,
	options ...grpc.CallOption,
) (filev1.FileService_DownloadStreamClient, error) {
	return client.client.DownloadStream(ctx, request, options...)
}

// GetThumb starts one thumbnail stream.
func (client *FileClient) GetThumb(
	ctx context.Context,
	request *filev1.GetThumbRequest,
	options ...grpc.CallOption,
) (filev1.FileService_GetThumbClient, error) {
	return client.client.GetThumb(ctx, request, options...)
}

// RenameFile updates one display name.
func (client *FileClient) RenameFile(
	ctx context.Context,
	request *filev1.RenameFileRequest,
	options ...grpc.CallOption,
) (*filev1.RenameFileResponse, error) {
	return client.client.RenameFile(ctx, request, options...)
}

// DeleteFile removes one attachment.
func (client *FileClient) DeleteFile(
	ctx context.Context,
	request *filev1.DeleteFileRequest,
	options ...grpc.CallOption,
) (*emptypb.Empty, error) {
	return client.client.DeleteFile(ctx, request, options...)
}
