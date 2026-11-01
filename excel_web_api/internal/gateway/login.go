// gateway/login.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
)

func (routes *authRoutes) login(w http.ResponseWriter, request *http.Request) {
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, request, &payload); err != nil {
		writeCodeError(w, deal.CodeInvalidArgument)
		return
	}
	response, err := routes.client.Login(outgoingClientContext(request), &iamv1.LoginRequest{
		Username: payload.Username,
		Password: payload.Password,
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
	setSessionCookie(w, response.GetToken())
	writeSuccess(w, data)
}
