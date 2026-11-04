// gateway/comment_routes.go：H-57 ~ H-62 评论路由。
package gateway

import (
	"net/http"
	"strings"

	commentv1 "excel/api/comment/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type commentRoutes struct {
	pipeline *Pipeline
	client   rpc.CommentRPC
}

// RegisterCommentRoutes binds the two-level comment endpoints behind SessionRequired.
func RegisterCommentRoutes(pipeline *Pipeline, client rpc.CommentRPC) {
	if pipeline == nil {
		panic("comment routes pipeline is required")
	}
	routes := &commentRoutes{pipeline: pipeline, client: client}
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/records/{record_id}/comments",
		http.HandlerFunc(routes.listComments),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/records/{record_id}/comments",
		http.HandlerFunc(routes.createComment),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/comments/{comment_id}/replies",
		http.HandlerFunc(routes.replyComment),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/comments/{comment_id}/resolve",
		http.HandlerFunc(routes.resolveComment),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/comments/{comment_id}/reopen",
		http.HandlerFunc(routes.reopenComment),
	)
	pipeline.SessionRequired(
		http.MethodDelete,
		"/api/v1/comments/{comment_id}",
		http.HandlerFunc(routes.deleteComment),
	)
}

func (routes *commentRoutes) listComments(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	recordID := strings.TrimSpace(request.PathValue("record_id"))
	if recordID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ListComments(request.Context(), &commentv1.ListCommentsRequest{
		RecordId: recordID,
		Cursor:   queryString(request, "cursor", ""),
		Limit:    queryLimit(request, 0, 100),
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *commentRoutes) createComment(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		Content     string   `json:"content"`
		MentionList []string `json:"mention_list"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Content) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.CreateComment(request.Context(), &commentv1.CreateCommentRequest{
		RecordId:    request.PathValue("record_id"),
		Content:     body.Content,
		MentionList: body.MentionList,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *commentRoutes) replyComment(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	commentID := strings.TrimSpace(request.PathValue("comment_id"))
	var body struct {
		Content     string   `json:"content"`
		MentionList []string `json:"mention_list"`
	}
	if commentID == "" ||
		decodeJSONBody(response, request, &body) != nil ||
		strings.TrimSpace(body.Content) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ReplyComment(request.Context(), &commentv1.ReplyCommentRequest{
		CommentId:   commentID,
		Content:     body.Content,
		MentionList: body.MentionList,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *commentRoutes) resolveComment(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	commentID := strings.TrimSpace(request.PathValue("comment_id"))
	if commentID == "" || decodeOptionalJSONBody(response, request, &struct{}{}) != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ResolveComment(request.Context(), &commentv1.ResolveCommentRequest{
		CommentId: commentID,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *commentRoutes) reopenComment(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	commentID := strings.TrimSpace(request.PathValue("comment_id"))
	if commentID == "" || decodeOptionalJSONBody(response, request, &struct{}{}) != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ReopenComment(request.Context(), &commentv1.ReopenCommentRequest{
		CommentId: commentID,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *commentRoutes) deleteComment(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	commentID := strings.TrimSpace(request.PathValue("comment_id"))
	if commentID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	if _, err := routes.client.DeleteComment(request.Context(), &commentv1.DeleteCommentRequest{
		CommentId: commentID,
	}); err != nil {
		writeBusinessError(response, err)
		return
	}
	writeSuccess(response, map[string]any{})
}
