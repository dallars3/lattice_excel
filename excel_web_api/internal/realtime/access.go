// realtime/access.go：房间握手裁决缝（Base/表存在性与访问权）。
package realtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	commonv1 "excel/api/common/v1"
)

// ErrRoomNotFound 表示 Base 或数据表不存在（或对当前身份不可见），握手以 404 拒绝。
var ErrRoomNotFound = errors.New("realtime: base or table not found")

// ErrRoomForbidden 表示身份对 Base 无访问权，握手以 403 拒绝。
var ErrRoomForbidden = errors.New("realtime: base access denied")

// RoomAuthorizer validates Base/table existence and access before the WS
// upgrade. Production adapters back it with share.CheckAccess（唯一角色裁决口，
// CODING_STANDARDS §4.1）；测试以 fake 注入。
type RoomAuthorizer interface {
	AuthorizeRoom(
		ctx context.Context,
		principal *commonv1.Principal,
		baseID, tableID string,
	) error
}

// handshakeTimeout bounds each upgrade authorization so an unreachable
// dependency rejects the handshake instead of hanging the dialer.
const handshakeTimeout = 5 * time.Second

// authorizeRoom 在升级前校验路由携带的 Base/表；两者都缺省视为裸挂载（本地
// 测试/内嵌部署），交由 join 帧的路由一致性校验兜底，不做裁决。
func (hub *Hub) authorizeRoom(request *http.Request, principal *commonv1.Principal) error {
	if hub.access == nil {
		return nil
	}
	baseID := routeValue(request, "base_id")
	tableID := routeValue(request, "table_id")
	if baseID == "" && tableID == "" {
		return nil
	}
	if baseID == "" || tableID == "" {
		return ErrRoomNotFound
	}
	ctx, cancel := context.WithTimeout(request.Context(), handshakeTimeout)
	defer cancel()
	return hub.access.AuthorizeRoom(ctx, principal, baseID, tableID)
}

// HandshakeStatus maps an AuthorizeRoom verdict onto the pre-upgrade HTTP
// rejection status：不存在 404、无权 403、裁决依赖不可用 503（fail-closed）。
func HandshakeStatus(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrRoomNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrRoomForbidden):
		return http.StatusForbidden
	default:
		return http.StatusServiceUnavailable
	}
}

// handshakeRejection 把裁决结果映射为升级前的 HTTP 拒绝状态码：不存在 404、
// 无权 403、裁决依赖不可用 503（fail-closed，绝不带错升级）。
func handshakeRejection(response http.ResponseWriter, err error) {
	slog.Warn("realtime handshake rejected", "status", HandshakeStatus(err), "reason", err.Error())
	http.Error(response, "realtime handshake rejected", HandshakeStatus(err))
}
