// gateway/logout.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
)

func (routes *authRoutes) logout(w http.ResponseWriter, request *http.Request) {
	token := sessionToken(request)
	if token == "" {
		writeCodeError(w, deal.CodeUnauthenticated)
		return
	}
	if _, err := routes.client.Logout(request.Context(), &iamv1.LogoutRequest{Token: token}); err != nil {
		writeBusinessError(w, err)
		return
	}
	clearSessionCookie(w)
	writeSuccess(w, map[string]any{})
}
