// deal/common.go：本包可复用逻辑归口。
package deal

import (
	"context"
	"encoding/base64"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
)

// PrincipalFromContext extracts the gateway-injected principal from incoming
// gRPC metadata（"x-principal" 携带 base64 编码的 common.v1.Principal）。
func PrincipalFromContext(ctx context.Context) (*commonv1.Principal, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, RPCError(CodeUnauthenticated)
	}
	values := incoming.Get("x-principal")
	if len(values) == 0 {
		return nil, RPCError(CodeUnauthenticated)
	}
	payload, err := base64.StdEncoding.DecodeString(values[0])
	if err != nil {
		return nil, RPCError(CodeUnauthenticated)
	}
	var principal commonv1.Principal
	if err := proto.Unmarshal(payload, &principal); err != nil || principal.GetUserId() == "" {
		return nil, RPCError(CodeUnauthenticated)
	}
	return &principal, nil
}
