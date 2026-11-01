// gateway/change_password.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
)

func (routes *authRoutes) changePassword(w http.ResponseWriter, request *http.Request) {
	ctx, err := routes.authenticatedContext(request)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	var payload struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSONBody(w, request, &payload); err != nil {
		writeCodeError(w, deal.CodeInvalidArgument)
		return
	}
	if _, err := routes.client.ChangePassword(ctx, &iamv1.ChangePasswordRequest{
		OldPassword: payload.OldPassword,
		NewPassword: payload.NewPassword,
	}); err != nil {
		writeBusinessError(w, err)
		return
	}
	writeSuccess(w, map[string]any{})
}
