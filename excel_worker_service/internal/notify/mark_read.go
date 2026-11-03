// notify/mark_read.go：幂等单条已读。
package notify

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	notifyv1 "excel/api/notify/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// MarkRead marks one notification read. Replays have no side effects.
func (service *Service) MarkRead(
	ctx context.Context,
	request *notifyv1.MarkReadRequest,
) (*emptypb.Empty, error) {
	if service == nil || service.repository == nil || service.counters == nil ||
		request == nil || request.GetNotificationId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := service.repository.MarkReadTx(
		ctx,
		tx,
		principal.GetUserId(),
		request.GetNotificationId(),
	)
	switch {
	case errors.Is(err, base.ErrNotifyNotFound):
		return nil, rpcError(deal.CodeDeletedResource)
	case errors.Is(err, base.ErrNotifyForbidden):
		return nil, rpcError(deal.CodePermissionDenied)
	case err != nil:
		return nil, err
	}
	if changed {
		if err := service.counters.Decrement(ctx, principal.GetUserId()); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		if changed {
			_ = service.counters.Increment(ctx, principal.GetUserId())
		}
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
