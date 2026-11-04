package deal

import (
	"log/slog"
	"strconv"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	CodeInvalidArgument      = 1001
	CodeUnauthenticated      = 2001
	CodePermissionDenied     = 3001
	CodeExternalExportDenied = 3002
	CodeConcurrentConflict   = 4001
	CodeDeletedResource      = 4002
	CodeCapacityLimit        = 5001
	CodeFileRejected         = 5002
	CodeRateLimited          = 5003
	CodeServiceUnavailable   = 9001
)

// ErrorSpec is the transport-neutral contract for a business error code.
type ErrorSpec struct {
	Code       int
	Message    string
	HTTPStatus int
	GRPCCode   codes.Code
}

// Envelope is the unified client-facing error body.
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

var errorCatalog = map[int]ErrorSpec{
	CodeInvalidArgument:      {Code: CodeInvalidArgument, Message: "参数缺失或非法", HTTPStatus: 400, GRPCCode: codes.InvalidArgument},
	CodeUnauthenticated:      {Code: CodeUnauthenticated, Message: "未登录", HTTPStatus: 401, GRPCCode: codes.Unauthenticated},
	CodePermissionDenied:     {Code: CodePermissionDenied, Message: "角色无此操作权限", HTTPStatus: 403, GRPCCode: codes.PermissionDenied},
	CodeExternalExportDenied: {Code: CodeExternalExportDenied, Message: "外部协作者禁导出", HTTPStatus: 403, GRPCCode: codes.PermissionDenied},
	CodeConcurrentConflict:   {Code: CodeConcurrentConflict, Message: "并发版本冲突", HTTPStatus: 409, GRPCCode: codes.Aborted},
	CodeDeletedResource:      {Code: CodeDeletedResource, Message: "记录/字段已被删除", HTTPStatus: 409, GRPCCode: codes.NotFound},
	CodeCapacityLimit:        {Code: CodeCapacityLimit, Message: "容量软上限", HTTPStatus: 413, GRPCCode: codes.ResourceExhausted},
	CodeFileRejected:         {Code: CodeFileRejected, Message: "文件超限/黑名单", HTTPStatus: 413, GRPCCode: codes.InvalidArgument},
	CodeRateLimited:          {Code: CodeRateLimited, Message: "触发限速", HTTPStatus: 429, GRPCCode: codes.ResourceExhausted},
	CodeServiceUnavailable:   {Code: CodeServiceUnavailable, Message: "服务暂不可用", HTTPStatus: 503, GRPCCode: codes.Unavailable},
}

// LookupError returns the contract for code.
func LookupError(code int) (ErrorSpec, bool) {
	spec, ok := errorCatalog[code]
	return spec, ok
}

// NewError creates the unified error envelope for code.
func NewError(code int, data any) Envelope {
	spec := ErrorSpecFor(code)
	return Envelope{Code: code, Message: spec.Message, Data: data}
}

// ErrorSpecFor returns the catalog spec for code; unknown codes resolve to the
// service-unavailable spec（含告警日志），调用方永远不会拿到零值 spec。
func ErrorSpecFor(code int) ErrorSpec {
	spec, ok := errorCatalog[code]
	if !ok {
		slog.Warn("deal: unknown business code, fallback to service-unavailable", "code", code)
		spec = errorCatalog[CodeServiceUnavailable]
	}
	return spec
}

// RPCError builds the unified gRPC error for a business code；业务码经 ErrorInfo
// reason 携带，供网关转译。
func RPCError(code int) error {
	spec := ErrorSpecFor(code)
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(
		&errdetails.ErrorInfo{Reason: strconv.Itoa(code)},
	)
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}
