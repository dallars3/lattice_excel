// base/comment_cleanup.go：Base 级联删除的评论侧清理。
package base

import (
	"context"
	"errors"
	"fmt"
)

// DeleteBaseComments removes every comment (threads and replies) whose record
// belongs to the Base. Soft-deleted records still belong to the Base and their
// comments are removed as well.
//
// 跨模块 SQL（§2.5）：评论表不冗余 Base 归属，只能经 records JOIN
// structure_tables 解析；无法解析的孤儿评论保持不动，避免误删其它 Base 数据。
func (repository *CommentRepository) DeleteBaseComments(
	ctx context.Context,
	baseID string,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("comment database is required")
	}
	if baseID == "" {
		return errors.New("Base id is required")
	}
	if _, err := repository.db.ExecContext(ctx, `
		DELETE comments
		FROM excel_comment_comments AS comments
		INNER JOIN excel_records AS records ON records.id = comments.record_id
		INNER JOIN excel_structure_tables AS tables ON tables.id = records.table_id
		WHERE tables.base_id = ?
	`, baseID); err != nil {
		return fmt.Errorf("delete Base %s comments: %w", baseID, err)
	}
	return nil
}
