// gateway/get_me.go：一文件一接口。细则见《excel_web_api-技术方案》§15。
package gateway

import (
	"net/http"

	"google.golang.org/protobuf/types/known/emptypb"

	"excel_web_api/deal"
)

func (routes *authRoutes) getMe(w http.ResponseWriter, request *http.Request) {
	ctx, err := routes.authenticatedContext(request)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	response, err := routes.client.GetMe(ctx, &emptypb.Empty{})
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
