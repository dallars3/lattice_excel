// comment/reopen_comment.go：重新打开已解决评论。
package comment

import (
	"context"
	"fmt"

	commentv1 "excel/api/comment/v1"
	"excel_core_service/deal"
)

// ReopenComment marks one comment open. Repeated reopens are idempotent.
func (service *Service) ReopenComment(
	ctx context.Context,
	request *commentv1.ReopenCommentRequest,
) (*commentv1.ReopenCommentResponse, error) {
	if service == nil || service.repository == nil ||
		request == nil || request.GetCommentId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if _, err := deal.PrincipalFromContext(ctx); err != nil {
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
	comment.Status = "OPEN"
	if err := service.repository.SetStatusTx(ctx, tx, comment.ID, comment.Status); err != nil {
		return nil, mapRepositoryError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ReopenComment: %w", err)
	}
	return &commentv1.ReopenCommentResponse{Comment: commentToProto(comment)}, nil
}
