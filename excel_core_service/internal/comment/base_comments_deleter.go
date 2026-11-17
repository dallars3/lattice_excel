// comment/base_comments_deleter.go：workspace 删除 Base 时的评论清理适配。
package comment

import (
	"context"

	"excel_core_service/base"
)

// BaseCommentsDeleter adapts the comment persistence cleanup to the workspace
// Base-deletion seam.
type BaseCommentsDeleter struct {
	repository *base.CommentRepository
}

// NewBaseCommentsDeleter creates the comment cleanup adapter used by workspace.
func NewBaseCommentsDeleter(repository *base.CommentRepository) *BaseCommentsDeleter {
	return &BaseCommentsDeleter{repository: repository}
}

// DeleteBaseComments removes every comment owned by the Base's records.
func (deleter *BaseCommentsDeleter) DeleteBaseComments(
	ctx context.Context,
	baseID string,
) error {
	return deleter.repository.DeleteBaseComments(ctx, baseID)
}
