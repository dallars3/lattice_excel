// comment/delete_comment.go：仅允许作者物理删除自己的评论。
package comment

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/emptypb"

	commentv1 "excel/api/comment/v1"
	"excel_core_service/deal"
)

// DeleteComment permanently deletes one comment authored by the caller.
func (service *Service) DeleteComment(
	ctx context.Context,
	request *commentv1.DeleteCommentRequest,
) (*emptypb.Empty, error) {
	if service == nil || service.repository == nil ||
		request == nil || request.GetCommentId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	comment, err := service.repository.CommentByIDForUpdate(ctx, tx, request.GetCommentId())
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if comment.Author != principal.GetUserId() {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err := service.repository.DeleteCommentTx(ctx, tx, comment); err != nil {
		return nil, mapRepositoryError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit DeleteComment: %w", err)
	}
	return &emptypb.Empty{}, nil
}
