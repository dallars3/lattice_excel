// rpc/common.go：core client 共享元数据适配。
package rpc

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

// ForwardMetadata copies selected gateway metadata to a core RPC context.
func ForwardMetadata(ctx context.Context, keys ...string) context.Context {
	incoming, _ := metadata.FromIncomingContext(ctx)
	outgoing := metadata.MD{}
	for key, values := range incoming {
		normalized := strings.ToLower(key)
		for _, allowed := range keys {
			if normalized == strings.ToLower(allowed) {
				outgoing[key] = append([]string(nil), values...)
				break
			}
		}
	}
	return metadata.NewOutgoingContext(ctx, outgoing)
}
