package deal

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
)

func TestErrorCatalogContract(t *testing.T) {
	tests := []struct {
		code       int
		message    string
		httpStatus int
		grpcCode   codes.Code
	}{
		{1001, "参数缺失或非法", 400, codes.InvalidArgument},
		{2001, "未登录", 401, codes.Unauthenticated},
		{3001, "角色无此操作权限", 403, codes.PermissionDenied},
		{3002, "外部协作者禁导出", 403, codes.PermissionDenied},
		{4001, "并发版本冲突", 409, codes.Aborted},
		{4002, "记录/字段已被删除", 409, codes.NotFound},
		{5001, "容量软上限", 413, codes.ResourceExhausted},
		{5002, "文件超限/黑名单", 413, codes.InvalidArgument},
		{5003, "触发限速", 429, codes.ResourceExhausted},
		{9001, "服务暂不可用", 503, codes.Unavailable},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			spec, ok := LookupError(tt.code)
			if !ok {
				t.Fatalf("LookupError(%d) not found", tt.code)
			}
			if spec.Message != tt.message {
				t.Errorf("Message = %q, want %q", spec.Message, tt.message)
			}
			if spec.HTTPStatus != tt.httpStatus {
				t.Errorf("HTTPStatus = %d, want %d", spec.HTTPStatus, tt.httpStatus)
			}
			if spec.GRPCCode != tt.grpcCode {
				t.Errorf("GRPCCode = %v, want %v", spec.GRPCCode, tt.grpcCode)
			}
		})
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	payload, err := json.Marshal(NewError(9001, nil))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"code":9001,"message":"服务暂不可用","data":null}`
	if string(payload) != want {
		t.Fatalf("envelope = %s, want %s", payload, want)
	}
}
