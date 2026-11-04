// rpc/comment.go：该模块 gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	commentv1 "excel/api/comment/v1"
)

// CommentRPC is the comment subset consumed by the HTTP gateway.
type CommentRPC interface {
	ListComments(context.Context, *commentv1.ListCommentsRequest) (*commentv1.ListCommentsResponse, error)
	CreateComment(context.Context, *commentv1.CreateCommentRequest) (*commentv1.CreateCommentResponse, error)
	ReplyComment(context.Context, *commentv1.ReplyCommentRequest) (*commentv1.ReplyCommentResponse, error)
	ResolveComment(context.Context, *commentv1.ResolveCommentRequest) (*commentv1.ResolveCommentResponse, error)
	ReopenComment(context.Context, *commentv1.ReopenCommentRequest) (*commentv1.ReopenCommentResponse, error)
	DeleteComment(context.Context, *commentv1.DeleteCommentRequest) (*emptypb.Empty, error)
}

// CommentClient adapts the generated gRPC client to CommentRPC.
type CommentClient struct {
	client commentv1.CommentServiceClient
}

var _ CommentRPC = (*CommentClient)(nil)

// NewCommentClient wraps a core-service gRPC connection.
func NewCommentClient(connection grpc.ClientConnInterface) *CommentClient {
	return &CommentClient{client: commentv1.NewCommentServiceClient(connection)}
}

// ListComments forwards one comment page lookup.
func (client *CommentClient) ListComments(
	ctx context.Context,
	request *commentv1.ListCommentsRequest,
) (*commentv1.ListCommentsResponse, error) {
	return client.client.ListComments(ctx, request)
}

// CreateComment forwards one top-level comment creation.
func (client *CommentClient) CreateComment(
	ctx context.Context,
	request *commentv1.CreateCommentRequest,
) (*commentv1.CreateCommentResponse, error) {
	return client.client.CreateComment(ctx, request)
}

// ReplyComment forwards one nested reply.
func (client *CommentClient) ReplyComment(
	ctx context.Context,
	request *commentv1.ReplyCommentRequest,
) (*commentv1.ReplyCommentResponse, error) {
	return client.client.ReplyComment(ctx, request)
}

// ResolveComment forwards one comment resolution.
func (client *CommentClient) ResolveComment(
	ctx context.Context,
	request *commentv1.ResolveCommentRequest,
) (*commentv1.ResolveCommentResponse, error) {
	return client.client.ResolveComment(ctx, request)
}

// ReopenComment forwards one comment reopen.
func (client *CommentClient) ReopenComment(
	ctx context.Context,
	request *commentv1.ReopenCommentRequest,
) (*commentv1.ReopenCommentResponse, error) {
	return client.client.ReopenComment(ctx, request)
}

// DeleteComment forwards one comment deletion.
func (client *CommentClient) DeleteComment(
	ctx context.Context,
	request *commentv1.DeleteCommentRequest,
) (*emptypb.Empty, error) {
	return client.client.DeleteComment(ctx, request)
}
