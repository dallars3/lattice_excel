// comment/resolve_comment.go：将评论线程标记为已解决。
package comment

import (
	"context"
	"fmt"

	commentv1 "excel/api/comment/v1"
	"excel_core_service/deal"
)

// ResolveComment marks one comment resolved. Repeated resolves are idempotent.
func (service *Service) ResolveComment(
	ctx context.Context,
	request *commentv1.ResolveCommentRequest,
) (*commentv1.ResolveCommentResponse, error) {
	if service == nil || service.repository == nil ||
		request == nil || request.GetCommentId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if _, err := principalFromContext(ctx); err != nil {
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
	comment.Status = "RESOLVED"
	if err := service.repository.SetStatusTx(ctx, tx, comment.ID, comment.Status); err != nil {
		return nil, mapRepositoryError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ResolveComment: %w", err)
	}
	return &commentv1.ResolveCommentResponse{Comment: commentToProto(comment)}, nil
}
