// comment/delete_comment.go：允许评论作者或 Base 可管理者物理删除评论（spec §4）。
package comment

import (
	"context"
	"database/sql"
	"fmt"

	"google.golang.org/protobuf/types/known/emptypb"

	commentv1 "excel/api/comment/v1"
	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// DeleteComment permanently deletes one comment when the caller is its author
// or the Base manager.
func (service *Service) DeleteComment(
	ctx context.Context,
	request *commentv1.DeleteCommentRequest,
) (*emptypb.Empty, error) {
	if service == nil || service.repository == nil ||
		request == nil || request.GetCommentId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
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
	if err := service.authorizeCommentDelete(ctx, tx, principal, comment); err != nil {
		return nil, err
	}
	if err := service.repository.DeleteCommentTx(ctx, tx, comment); err != nil {
		return nil, mapRepositoryError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit DeleteComment: %w", err)
	}
	return &emptypb.Empty{}, nil
}

// authorizeCommentDelete 落实删除权限矩阵：作者恒可自删；非作者仅 Base 可管理
// 者可删，其余一律 3001（ADR 0003）。可管理判定按记录归属解析 Base（记录已软
// 删除时按 4002 拒绝，与记录域「不可操作」口径一致）。
func (service *Service) authorizeCommentDelete(
	ctx context.Context,
	tx *sql.Tx,
	principal *commonv1.Principal,
	comment base.Comment,
) error {
	if comment.Author == principal.GetUserId() {
		return nil
	}
	baseID, _, err := service.repository.RecordOwnershipForUpdate(ctx, tx, comment.RecordID)
	if err != nil {
		return mapRepositoryError(err)
	}
	role, err := service.baseRole(ctx, principal, baseID)
	if err != nil {
		return err
	}
	if role != sharev1.Role_ROLE_MANAGER {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	return nil
}
