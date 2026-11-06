// realtime/access_rpc.go：握手裁决的生产适配（share.CheckAccess + structure 探针）。
package realtime

import (
	"context"
	"encoding/base64"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	structurev1 "excel/api/structure/v1"
)

// RoomAccessRPC is the share seam for the canonical Base access decision
// (CODING_STANDARDS §4.1：CheckAccess 是唯一角色裁决口)。
type RoomAccessRPC interface {
	CheckAccess(
		context.Context,
		*sharev1.CheckAccessRequest,
	) (*sharev1.CheckAccessResponse, error)
}

// RoomTableProbeRPC validates table existence within the caller's tenancy via
// the cheap structure read path（一张表行 + 字段列表查询，不取记录数据）。
type RoomTableProbeRPC interface {
	ListFields(
		context.Context,
		*structurev1.ListFieldsRequest,
	) (*structurev1.ListFieldsResponse, error)
}

// RPCRoomAuthorizer rejects invalid rooms before the WS upgrade：Base 存在性
// 与访问权由 share.CheckAccess 裁决（不存在→codes.NotFound、无权→allow=false），
// 表存在性由 structure.ListFields 按租户校验。
type RPCRoomAuthorizer struct {
	share  RoomAccessRPC
	tables RoomTableProbeRPC
}

var _ RoomAuthorizer = (*RPCRoomAuthorizer)(nil)

// NewRPCRoomAuthorizer creates the production handshake authorizer.
func NewRPCRoomAuthorizer(share RoomAccessRPC, tables RoomTableProbeRPC) *RPCRoomAuthorizer {
	return &RPCRoomAuthorizer{share: share, tables: tables}
}

// AuthorizeRoom implements RoomAuthorizer.
func (authorizer *RPCRoomAuthorizer) AuthorizeRoom(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID, tableID string,
) error {
	baseID = strings.TrimSpace(baseID)
	tableID = strings.TrimSpace(tableID)
	if baseID == "" || tableID == "" {
		return ErrRoomNotFound
	}
	decision, err := authorizer.share.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal:   principal,
		BaseId:      baseID,
		Action:      sharev1.Action_ACTION_VIEW,
		LinkContext: principal.GetLinkContext(),
	})
	if err != nil {
		return handshakeRPCError(err)
	}
	if decision == nil || !decision.GetAllow() {
		return ErrRoomForbidden
	}
	probeCtx, err := roomProbeContext(ctx, principal)
	if err != nil {
		return err
	}
	if _, err := authorizer.tables.ListFields(probeCtx, &structurev1.ListFieldsRequest{
		TableId: tableID,
	}); err != nil {
		return handshakeRPCError(err)
	}
	return nil
}

// roomProbeContext 把身份折进探针调用（与 gateway.resolvePrincipal 同一
// x-principal base64 proto 传输约定）。
func roomProbeContext(ctx context.Context, principal *commonv1.Principal) (context.Context, error) {
	payload, err := proto.Marshal(principal)
	if err != nil {
		return nil, status.Error(codes.Internal, "principal marshal failed")
	}
	return metadata.AppendToOutgoingContext(
		ctx,
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	), nil
}

// handshakeRPCError 把契约 RPC 错误折叠为握手哨兵：不存在 404、无权 403、
// 参数非法按不可见处理（404，不泄露存在性）；基础设施故障原样上抛，由 hub
// 以 503 fail-closed。
func handshakeRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return ErrRoomNotFound
	case codes.PermissionDenied:
		return ErrRoomForbidden
	case codes.InvalidArgument:
		return ErrRoomNotFound
	default:
		return err
	}
}
