// comment/list_comments.go：按顶层线程分页返回两级评论。
package comment

import (
	"context"
	"errors"

	commentv1 "excel/api/comment/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ListComments returns top-level threads in reverse chronological order, with
// each thread's replies in chronological order.
func (service *Service) ListComments(
	ctx context.Context,
	request *commentv1.ListCommentsRequest,
) (*commentv1.ListCommentsResponse, error) {
	if service == nil || service.repository == nil ||
		request == nil || request.GetRecordId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if _, err := deal.PrincipalFromContext(ctx); err != nil {
		return nil, err
	}
	limit, err := normalizeCommentLimit(request.GetLimit())
	if err != nil {
		return nil, err
	}
	var cursor *base.CommentCursor
	if request.GetCursor() != "" {
		last, err := service.repository.CommentByID(ctx, request.GetCursor())
		if err != nil || last.RecordID != request.GetRecordId() || last.ParentID != "" {
			if errors.Is(err, base.ErrCommentNotFound) || err == nil {
				return nil, deal.RPCError(deal.CodeInvalidArgument)
			}
			return nil, mapRepositoryError(err)
		}
		cursor = &base.CommentCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	roots, err := service.repository.TopLevelPage(
		ctx,
		request.GetRecordId(),
		cursor,
		limit,
	)
	if err != nil {
		return nil, err
	}
	hasMore := len(roots) > limit
	if hasMore {
		roots = roots[:limit]
	}
	parentIDs := make([]string, 0, len(roots))
	for _, root := range roots {
		parentIDs = append(parentIDs, root.ID)
	}
	replies, err := service.repository.RepliesByParentIDs(ctx, parentIDs)
	if err != nil {
		return nil, err
	}
	repliesByParent := make(map[string][]base.Comment)
	for _, reply := range replies {
		repliesByParent[reply.ParentID] = append(repliesByParent[reply.ParentID], reply)
	}
	commentList := make([]*commentv1.Comment, 0, len(roots)+len(replies))
	for _, root := range roots {
		commentList = append(commentList, commentToProto(root))
		for _, reply := range repliesByParent[root.ID] {
			commentList = append(commentList, commentToProto(reply))
		}
	}
	unsolvedCount, err := service.repository.UnsolvedCount(ctx, request.GetRecordId())
	if err != nil {
		return nil, err
	}
	response := &commentv1.ListCommentsResponse{
		CommentList:   commentList,
		UnsolvedCount: unsolvedCount,
		HasMore:       hasMore,
	}
	if hasMore && len(roots) > 0 {
		response.NextCursor = roots[len(roots)-1].ID
	}
	return response, nil
}
