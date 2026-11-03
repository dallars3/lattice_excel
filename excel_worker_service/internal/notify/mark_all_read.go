// notify/mark_all_read.go：幂等全部已读并清零未读投影。
package notify

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"excel_worker_service/deal"
)

// MarkAllRead marks every retained notification read.
func (service *Service) MarkAllRead(
	ctx context.Context,
	_ *emptypb.Empty,
) (*emptypb.Empty, error) {
	if service == nil || service.repository == nil || service.counters == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
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
	if _, err := service.repository.MarkAllReadTx(
		ctx,
		tx,
		principal.GetUserId(),
		service.retentionCutoff(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := service.counters.Clear(ctx, principal.GetUserId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
