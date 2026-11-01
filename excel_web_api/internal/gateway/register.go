// gateway/register.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
)

func (routes *authRoutes) register(w http.ResponseWriter, request *http.Request) {
	var payload struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		EnterpriseName string `json:"enterprise_name"`
		LinkToken      string `json:"link_token"`
	}
	if err := decodeJSONBody(w, request, &payload); err != nil {
		writeCodeError(w, deal.CodeInvalidArgument)
		return
	}
	response, err := routes.client.Register(request.Context(), &iamv1.RegisterRequest{
		Username:       payload.Username,
		Password:       payload.Password,
		EnterpriseName: payload.EnterpriseName,
		LinkToken:      payload.LinkToken,
	})
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	data, err := protoJSON(response)
	if err != nil {
		writeCodeError(w, deal.CodeServiceUnavailable)
		return
	}
	writeSuccess(w, data)
}
