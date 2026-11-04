// comment/reply_comment.go：创建第二级回复并记录实际被回复作者。
package comment

import (
	"context"
	"fmt"

	"excel-libs/idgen"
	commentv1 "excel/api/comment/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ReplyComment adds one second-level reply to a top-level comment thread.
func (service *Service) ReplyComment(
	ctx context.Context,
	request *commentv1.ReplyCommentRequest,
) (*commentv1.ReplyCommentResponse, error) {
	if service == nil || service.repository == nil || service.outbox == nil ||
		request == nil || request.GetCommentId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateContent(request.GetContent()); err != nil {
		return nil, err
	}
	mentions, err := normalizeMentions(request.GetMentionList())
	if err != nil {
		return nil, err
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	parent, err := service.repository.CommentByIDForUpdate(ctx, tx, request.GetCommentId())
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	baseID, err := service.repository.RecordBaseIDForUpdate(ctx, tx, parent.RecordID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	rootID := parent.ID
	if parent.ParentID != "" {
		rootID = parent.ParentID
	}
	commentID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	reply := base.Comment{
		ID:        commentID,
		BaseID:    baseID,
		RecordID:  parent.RecordID,
		ParentID:  rootID,
		Content:   request.GetContent(),
		Author:    principal.GetUserId(),
		Status:    "OPEN",
		Mentions:  mentions,
		CreatedAt: service.now().UTC().UnixMilli(),
	}
	if err := service.repository.CreateTx(ctx, tx, reply); err != nil {
		return nil, err
	}
	if err := service.enqueueCreatedEvents(ctx, tx, baseID, reply, parent.Author); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ReplyComment: %w", err)
	}
	return &commentv1.ReplyCommentResponse{Comment: commentToProto(reply)}, nil
}
