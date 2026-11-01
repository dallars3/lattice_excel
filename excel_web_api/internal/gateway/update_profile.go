// gateway/update_profile.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
)

func (routes *authRoutes) updateProfile(w http.ResponseWriter, request *http.Request) {
	ctx, err := routes.authenticatedContext(request)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	var payload struct {
		DisplayName string `json:"display_name"`
		AvatarURI   string `json:"avatar_uri"`
	}
	if err := decodeJSONBody(w, request, &payload); err != nil {
		writeCodeError(w, deal.CodeInvalidArgument)
		return
	}
	response, err := routes.client.UpdateProfile(ctx, &iamv1.UpdateProfileRequest{
		DisplayName: payload.DisplayName,
		AvatarUri:   payload.AvatarURI,
	})
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	data, err := protoJSON(response.GetUser())
	if err != nil {
		writeCodeError(w, deal.CodeServiceUnavailable)
		return
	}
	writeSuccess(w, data)
}
