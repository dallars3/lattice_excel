// comment/create_comment.go：创建一级评论并写入同名事务 outbox。
package comment

import (
	"context"
	"fmt"

	"excel-libs/idgen"
	commentv1 "excel/api/comment/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateComment creates one top-level comment on a record.
func (service *Service) CreateComment(
	ctx context.Context,
	request *commentv1.CreateCommentRequest,
) (*commentv1.CreateCommentResponse, error) {
	if service == nil || service.repository == nil || service.outbox == nil ||
		request == nil || request.GetRecordId() == "" {
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
	baseID, err := service.repository.RecordBaseIDForUpdate(ctx, tx, request.GetRecordId())
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	commentID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	comment := base.Comment{
		ID:        commentID,
		BaseID:    baseID,
		RecordID:  request.GetRecordId(),
		Content:   request.GetContent(),
		Author:    principal.GetUserId(),
		Status:    "OPEN",
		Mentions:  mentions,
		CreatedAt: service.now().UTC().UnixMilli(),
	}
	if err := service.repository.CreateTx(ctx, tx, comment); err != nil {
		return nil, err
	}
	if err := service.enqueueCreatedEvents(ctx, tx, baseID, comment, ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit CreateComment: %w", err)
	}
	return &commentv1.CreateCommentResponse{Comment: commentToProto(comment)}, nil
}
