// notify/get_unread_count.go：Redis 未读投影与 MySQL 自愈。
package notify

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	notifyv1 "excel/api/notify/v1"
	"excel_worker_service/deal"
)

// GetUnreadCount returns the caller's retained unread count.
func (service *Service) GetUnreadCount(
	ctx context.Context,
	_ *emptypb.Empty,
) (*notifyv1.GetUnreadCountResponse, error) {
	if service == nil || service.repository == nil || service.counters == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	count, found, err := service.counters.Lookup(ctx, principal.GetUserId())
	if err != nil {
		return nil, err
	}
	if !found {
		count, err = service.repository.UnreadCount(
			ctx,
			principal.GetUserId(),
			service.retentionCutoff(),
		)
		if err != nil {
			return nil, err
		}
		if err := service.counters.Set(ctx, principal.GetUserId(), count); err != nil {
			return nil, err
		}
	}
	return &notifyv1.GetUnreadCountResponse{Count: int32(count)}, nil
}
