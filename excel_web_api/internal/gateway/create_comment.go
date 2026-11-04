// gateway/create_comment.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"
	"strings"

	commentv1 "excel/api/comment/v1"
	"excel_web_api/deal"
)

// createComment handles POST /api/v1/records/{record_id}/comments.
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
