// notify/list_notifications.go：90 天保留期内的用户通知列表。
package notify

import (
	"context"

	notifyv1 "excel/api/notify/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// ListNotifications returns the caller's retained notifications newest first.
func (service *Service) ListNotifications(
	ctx context.Context,
	request *notifyv1.ListNotificationsRequest,
) (*notifyv1.ListNotificationsResponse, error) {
	if service == nil || service.repository == nil || request == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	limit, err := normalizeLimit(request.GetLimit())
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(request.GetCursor())
	if err != nil {
		return nil, err
	}
	rows, err := service.repository.List(
		ctx,
		principal.GetUserId(),
		service.retentionCutoff(),
		cursor,
		limit,
	)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	response := &notifyv1.ListNotificationsResponse{HasMore: hasMore}
	for _, row := range rows {
		notification, err := notificationToProto(row)
		if err != nil {
			return nil, err
		}
		response.NotificationList = append(response.NotificationList, notification)
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		response.NextCursor = encodeCursor(base.NotificationCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
	}
	return response, nil
}
